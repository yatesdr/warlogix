// Package brokertest provides stress testing for message broker connections.
package brokertest

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"warlogix/config"
	"warlogix/kafka"
	"warlogix/mqtt"
	"warlogix/valkey"
)

// toKafkaConfig converts a config.KafkaConfig to a kafka.Config.
func toKafkaConfig(cfg *config.KafkaConfig) *kafka.Config {
	autoCreate := true
	if cfg.AutoCreateTopics != nil {
		autoCreate = *cfg.AutoCreateTopics
	}
	return &kafka.Config{
		Name:             cfg.Name,
		Enabled:          cfg.Enabled,
		Brokers:          cfg.Brokers,
		UseTLS:           cfg.UseTLS,
		TLSSkipVerify:    cfg.TLSSkipVerify,
		SASLMechanism:    kafka.SASLMechanism(cfg.SASLMechanism),
		Username:         cfg.Username,
		Password:         cfg.Password,
		RequiredAcks:     cfg.RequiredAcks,
		MaxRetries:       cfg.MaxRetries,
		RetryBackoff:     cfg.RetryBackoff,
		PublishChanges:   cfg.PublishChanges,
		Topic:            cfg.Topic,
		AutoCreateTopics: autoCreate,
	}
}

// TestConfig holds configuration for the broker stress test.
type TestConfig struct {
	// Duration is how long to run each test
	Duration time.Duration
	// NumTags is the number of simulated tags to publish
	NumTags int
	// NumPLCs is the number of simulated PLCs
	NumPLCs int
}

// DefaultTestConfig returns sensible defaults for stress testing.
func DefaultTestConfig() TestConfig {
	return TestConfig{
		Duration: 10 * time.Second,
		NumTags:  100,
		NumPLCs:  50,
	}
}

// TestResult holds the results from a broker stress test.
type TestResult struct {
	BrokerType   string
	BrokerName   string
	Address      string
	Duration     time.Duration
	MessagesSent int64
	MessagesAcked int64
	Errors       int64
	Throughput   float64 // messages per second
	AvgLatency   time.Duration
	P50Latency   time.Duration
	P95Latency   time.Duration
	P99Latency   time.Duration
	MaxLatency   time.Duration
	Success      bool
	Error        error
}

// Runner executes broker stress tests.
type Runner struct {
	cfg      *config.Config
	testCfg  TestConfig
	results  []TestResult
}

// NewRunner creates a new stress test runner.
func NewRunner(cfg *config.Config, testCfg TestConfig) *Runner {
	return &Runner{
		cfg:     cfg,
		testCfg: testCfg,
	}
}

// Run executes stress tests for all configured brokers.
func (r *Runner) Run() []TestResult {
	r.printHeader()

	// Test Kafka brokers
	for i := range r.cfg.Kafka {
		kafkaCfg := &r.cfg.Kafka[i]
		if kafkaCfg.Enabled {
			result := r.testKafka(toKafkaConfig(kafkaCfg))
			r.results = append(r.results, result)
		}
	}

	// Test MQTT brokers
	for i := range r.cfg.MQTT {
		mqttCfg := &r.cfg.MQTT[i]
		if mqttCfg.Enabled {
			result := r.testMQTT(mqttCfg)
			r.results = append(r.results, result)
		}
	}

	// Test Valkey/Redis servers
	for i := range r.cfg.Valkey {
		valkeyCfg := &r.cfg.Valkey[i]
		if valkeyCfg.Enabled {
			result := r.testValkey(valkeyCfg)
			r.results = append(r.results, result)
		}
	}

	// Print final report
	r.printReport()

	return r.results
}

func (r *Runner) printHeader() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    BROKER STRESS TEST                            ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Test Parameters:\n")
	fmt.Printf("    Duration:       %v\n", r.testCfg.Duration)
	fmt.Printf("    Simulated PLCs: %d\n", r.testCfg.NumPLCs)
	fmt.Printf("    Tags per PLC:   %d\n", r.testCfg.NumTags)
	fmt.Printf("    Total Tags:     %d\n", r.testCfg.NumPLCs*r.testCfg.NumTags)
	fmt.Println()
}

// testKafka runs stress test against a Kafka cluster.
func (r *Runner) testKafka(cfg *kafka.Config) TestResult {
	result := TestResult{
		BrokerType: "Kafka",
		BrokerName: cfg.Name,
		Address:    strings.Join(cfg.Brokers, ","),
	}

	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")
	fmt.Printf("  Testing: Kafka/%s\n", cfg.Name)
	fmt.Printf("  Brokers: %s\n", result.Address)
	fmt.Printf("  Topic:   warlogix-test-stress\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config with test topic
	testCfg := *cfg
	testCfg.Topic = "warlogix-test-stress"
	testCfg.PublishChanges = true
	testCfg.AutoCreateTopics = true

	// Use the Manager for batched publishing (matches real-world usage)
	mgr := kafka.NewManager()
	mgr.AddCluster(&testCfg)
	if err := mgr.Connect(testCfg.Name); err != nil {
		result.Error = fmt.Errorf("connect failed: %w", err)
		fmt.Printf("  Status: FAILED - %v\n\n", result.Error)
		return result
	}
	defer mgr.StopAll()

	fmt.Printf("  Running... ")

	// Run the stress test using batched Manager.Publish
	result = r.runKafkaManagerStress(mgr, &testCfg, result)

	if result.Success {
		fmt.Printf("DONE\n\n")
	} else {
		fmt.Printf("FAILED\n\n")
	}

	return result
}

// runKafkaManagerStress executes a Kafka stress test using the batched Manager.
// This better reflects real-world usage where Publish() is called per tag change.
func (r *Runner) runKafkaManagerStress(mgr *kafka.Manager, cfg *kafka.Config, result TestResult) TestResult {
	var sent int64

	stopChan := make(chan struct{})
	time.AfterFunc(r.testCfg.Duration, func() { close(stopChan) })

	startTime := time.Now()

	// Simulate PLC tag changes (single-threaded like real polling)
	for {
		select {
		case <-stopChan:
			goto done
		default:
			plcNum := rand.Intn(r.testCfg.NumPLCs)
			tagNum := rand.Intn(r.testCfg.NumTags)
			plcName := fmt.Sprintf("TestPLC%d", plcNum)
			tagName := fmt.Sprintf("Tag%d", tagNum)
			value := rand.Intn(10000)

			// This queues to the batch processor (non-blocking)
			mgr.Publish(plcName, tagName, "", "", "DINT", value, false, true)
			atomic.AddInt64(&sent, 1)
		}
	}

done:
	// Allow time for batches to flush
	time.Sleep(100 * time.Millisecond)

	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent // Batched publish is fire-and-forget from caller's perspective

	// Get actual stats from producer
	producer := mgr.GetProducer(cfg.Name)
	if producer != nil {
		actualSent, actualErrors, _ := producer.GetStats()
		result.MessagesAcked = actualSent
		result.Errors = actualErrors
	}

	result.Throughput = float64(sent) / result.Duration.Seconds()
	result.Success = sent > 0 && result.Errors == 0

	return result
}

// runKafkaStress executes the actual Kafka stress test (direct producer, not batched).
func (r *Runner) runKafkaStress(producer *kafka.Producer, cfg *kafka.Config, result TestResult) TestResult {
	var sent, errors, contextErrors int64
	latencies := make([]time.Duration, 0, 100000)
	var latencyMu sync.Mutex

	// Use a stop channel instead of context for cleaner shutdown
	stopChan := make(chan struct{})
	time.AfterFunc(r.testCfg.Duration, func() { close(stopChan) })

	startTime := time.Now()

	// Generate test messages with multiple workers
	var wg sync.WaitGroup
	numWorkers := 4

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			localLatencies := make([]time.Duration, 0, 10000)

			for {
				select {
				case <-stopChan:
					// Merge local latencies
					latencyMu.Lock()
					latencies = append(latencies, localLatencies...)
					latencyMu.Unlock()
					return
				default:
					plcNum := rand.Intn(r.testCfg.NumPLCs)
					tagNum := rand.Intn(r.testCfg.NumTags)

					msg := map[string]interface{}{
						"plc":       fmt.Sprintf("TestPLC%d", plcNum),
						"tag":       fmt.Sprintf("Tag%d", tagNum),
						"value":     rand.Intn(10000),
						"type":      "DINT",
						"writable":  false,
						"timestamp": time.Now().UTC().Format(time.RFC3339Nano),
					}
					payload, _ := json.Marshal(msg)
					key := []byte(fmt.Sprintf("TestPLC%d.Tag%d", plcNum, tagNum))

					// Use a per-message context with generous timeout
					msgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					msgStart := time.Now()
					err := producer.Produce(msgCtx, cfg.Topic, key, payload)
					latency := time.Since(msgStart)
					cancel()

					if err != nil {
						if strings.Contains(err.Error(), "context") {
							atomic.AddInt64(&contextErrors, 1)
						}
						atomic.AddInt64(&errors, 1)
					} else {
						atomic.AddInt64(&sent, 1)
						localLatencies = append(localLatencies, latency)
					}
				}
			}
		}(w)
	}

	wg.Wait()

	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent // Kafka sync = sent == acked
	result.Errors = errors

	// Calculate throughput based on actual test duration
	result.Throughput = float64(sent) / result.Duration.Seconds()

	// Consider success if error rate is < 1%
	errorRate := float64(errors) / float64(sent+errors)
	result.Success = sent > 0 && errorRate < 0.01

	// Calculate latency percentiles
	if len(latencies) > 0 {
		result.AvgLatency, result.P50Latency, result.P95Latency, result.P99Latency, result.MaxLatency = calculateLatencyStats(latencies)
	}

	return result
}

// testMQTT runs stress test against an MQTT broker.
func (r *Runner) testMQTT(cfg *config.MQTTConfig) TestResult {
	result := TestResult{
		BrokerType: "MQTT",
		BrokerName: cfg.Name,
		Address:    fmt.Sprintf("%s:%d", cfg.Broker, cfg.Port),
	}

	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")
	fmt.Printf("  Testing: MQTT/%s\n", cfg.Name)
	fmt.Printf("  Broker:  %s\n", result.Address)
	fmt.Printf("  Topic:   warlogix-test-stress/+/tags/+\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config with test topic
	testCfg := *cfg
	testCfg.RootTopic = "warlogix-test-stress"
	testCfg.ClientID = fmt.Sprintf("warlogix-stress-%d", time.Now().UnixNano())

	// Create publisher
	pub := mqtt.NewPublisher(&testCfg)
	if err := pub.Start(); err != nil {
		result.Error = fmt.Errorf("connect failed: %w", err)
		fmt.Printf("  Status: FAILED - %v\n\n", result.Error)
		return result
	}
	defer pub.Stop()

	fmt.Printf("  Running... ")

	// Run the stress test
	result = r.runMQTTStress(pub, result)

	if result.Success {
		fmt.Printf("DONE\n\n")
	} else {
		fmt.Printf("FAILED\n\n")
	}

	return result
}

// runMQTTStress executes the actual MQTT stress test.
func (r *Runner) runMQTTStress(pub *mqtt.Publisher, result TestResult) TestResult {
	var sent, errors int64

	ctx, cancel := context.WithTimeout(context.Background(), r.testCfg.Duration)
	defer cancel()

	startTime := time.Now()

	// MQTT publishes are async, so we just count queued messages
	for {
		select {
		case <-ctx.Done():
			goto done
		default:
			plcNum := rand.Intn(r.testCfg.NumPLCs)
			tagNum := rand.Intn(r.testCfg.NumTags)
			plcName := fmt.Sprintf("TestPLC%d", plcNum)
			tagName := fmt.Sprintf("Tag%d", tagNum)
			value := rand.Intn(10000)

			// Force=true to always publish (bypass change detection)
			if pub.Publish(plcName, tagName, "", "", "DINT", value, false, true) {
				atomic.AddInt64(&sent, 1)
			} else {
				atomic.AddInt64(&errors, 1)
			}
		}
	}

done:
	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent // MQTT async - we assume sent = queued successfully
	result.Errors = errors
	result.Throughput = float64(sent) / result.Duration.Seconds()
	result.Success = errors == 0 && sent > 0

	// Note: MQTT is async so we can't measure per-message latency easily
	// The throughput represents queue rate, not delivery rate

	return result
}

// testValkey runs stress test against a Valkey/Redis server.
func (r *Runner) testValkey(cfg *config.ValkeyConfig) TestResult {
	result := TestResult{
		BrokerType: "Valkey",
		BrokerName: cfg.Name,
		Address:    cfg.Address,
	}

	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")
	fmt.Printf("  Testing: Valkey/%s\n", cfg.Name)
	fmt.Printf("  Server:  %s\n", result.Address)
	fmt.Printf("  Keys:    warlogix-test-stress:*\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config with test factory prefix
	testCfg := *cfg
	testCfg.Factory = "warlogix-test-stress"

	// Use the Manager for batched publishing (matches real-world usage)
	mgr := valkey.NewManager()
	mgr.Add(&testCfg)
	if err := mgr.Start(testCfg.Name); err != nil {
		result.Error = fmt.Errorf("connect failed: %w", err)
		fmt.Printf("  Status: FAILED - %v\n\n", result.Error)
		return result
	}
	defer mgr.StopAll()

	fmt.Printf("  Running... ")

	// Run the stress test using batched Manager.Publish
	result = r.runValkeyManagerStress(mgr, result)

	if result.Success {
		fmt.Printf("DONE\n\n")
	} else {
		fmt.Printf("FAILED\n\n")
	}

	return result
}

// runValkeyManagerStress executes a Valkey stress test using the batched Manager.
// This better reflects real-world usage where Publish() is called per tag change.
func (r *Runner) runValkeyManagerStress(mgr *valkey.Manager, result TestResult) TestResult {
	var sent int64

	stopChan := make(chan struct{})
	time.AfterFunc(r.testCfg.Duration, func() { close(stopChan) })

	startTime := time.Now()

	// Simulate PLC tag changes (single-threaded like real polling)
	for {
		select {
		case <-stopChan:
			goto done
		default:
			plcNum := rand.Intn(r.testCfg.NumPLCs)
			tagNum := rand.Intn(r.testCfg.NumTags)
			plcName := fmt.Sprintf("TestPLC%d", plcNum)
			tagName := fmt.Sprintf("Tag%d", tagNum)
			value := rand.Intn(10000)

			// This queues to the batch processor (non-blocking)
			mgr.Publish(plcName, tagName, "", "", "DINT", value, false)
			atomic.AddInt64(&sent, 1)
		}
	}

done:
	// Allow time for batches to flush
	time.Sleep(100 * time.Millisecond)

	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent // Batched publish is fire-and-forget from caller's perspective
	result.Throughput = float64(sent) / result.Duration.Seconds()
	result.Success = sent > 0

	return result
}

// calculateLatencyStats computes avg, p50, p95, p99, and max latencies.
func calculateLatencyStats(latencies []time.Duration) (avg, p50, p95, p99, max time.Duration) {
	if len(latencies) == 0 {
		return
	}

	// Sort for percentile calculation
	sorted := make([]time.Duration, len(latencies))
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

	// Calculate average
	var total time.Duration
	for _, l := range sorted {
		total += l
	}
	avg = total / time.Duration(len(sorted))

	// Percentiles
	p50 = sorted[len(sorted)*50/100]
	p95 = sorted[len(sorted)*95/100]
	p99 = sorted[len(sorted)*99/100]
	max = sorted[len(sorted)-1]

	return
}

// printReport prints a formatted summary report.
func (r *Runner) printReport() {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════════╗")
	fmt.Println("║                         TEST RESULTS                             ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════════╝")
	fmt.Println()

	if len(r.results) == 0 {
		fmt.Println("  No enabled brokers found in configuration.")
		fmt.Println()
		fmt.Println("  To run tests, enable brokers in ~/.warlogix/config.yaml:")
		fmt.Println("    - kafka[].enabled: true")
		fmt.Println("    - mqtt[].enabled: true")
		fmt.Println("    - valkey[].enabled: true")
		fmt.Println()
		return
	}

	// Summary table
	fmt.Println("  ┌─────────┬────────────────┬────────────────┬──────────────┬────────┐")
	fmt.Println("  │ Type    │ Name           │ Throughput     │ Messages     │ Status │")
	fmt.Println("  ├─────────┼────────────────┼────────────────┼──────────────┼────────┤")

	passed := 0
	failed := 0

	for _, result := range r.results {
		status := "✓ PASS"
		if !result.Success {
			status = "✗ FAIL"
			failed++
		} else {
			passed++
		}

		name := result.BrokerName
		if len(name) > 14 {
			name = name[:14]
		}

		throughput := fmt.Sprintf("%.0f msg/s", result.Throughput)
		messages := fmt.Sprintf("%d", result.MessagesSent)

		fmt.Printf("  │ %-7s │ %-14s │ %14s │ %12s │ %s │\n",
			result.BrokerType, name, throughput, messages, status)
	}

	fmt.Println("  └─────────┴────────────────┴────────────────┴──────────────┴────────┘")
	fmt.Println()

	// Detailed results
	for _, result := range r.results {
		if result.Error != nil {
			continue
		}

		fmt.Printf("  %s/%s:\n", result.BrokerType, result.BrokerName)
		fmt.Printf("    Address:    %s\n", result.Address)
		fmt.Printf("    Duration:   %v\n", result.Duration.Round(time.Millisecond))

		total := result.MessagesSent + result.Errors
		if result.Errors > 0 && total > 0 {
			errorRate := float64(result.Errors) / float64(total) * 100
			fmt.Printf("    Messages:   %d sent, %d errors (%.1f%% error rate)\n", result.MessagesSent, result.Errors, errorRate)
		} else {
			fmt.Printf("    Messages:   %d sent, %d errors\n", result.MessagesSent, result.Errors)
		}
		fmt.Printf("    Throughput: %.1f msg/s\n", result.Throughput)

		if result.AvgLatency > 0 {
			fmt.Printf("    Latency:\n")
			fmt.Printf("      avg: %v, p50: %v, p95: %v, p99: %v, max: %v\n",
				result.AvgLatency.Round(time.Microsecond),
				result.P50Latency.Round(time.Microsecond),
				result.P95Latency.Round(time.Microsecond),
				result.P99Latency.Round(time.Microsecond),
				result.MaxLatency.Round(time.Microsecond))
		}
		fmt.Println()
	}

	// Final summary
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	fmt.Printf("  Summary: %d passed, %d failed\n", passed, failed)

	if failed > 0 {
		fmt.Println()
		fmt.Println("  FAILED TESTS:")
		for _, result := range r.results {
			if !result.Success {
				errMsg := "unknown error"
				if result.Error != nil {
					errMsg = result.Error.Error()
				} else if result.Errors > 0 {
					errMsg = fmt.Sprintf("%d publish errors", result.Errors)
				}
				fmt.Printf("    - %s/%s: %s\n", result.BrokerType, result.BrokerName, errMsg)
			}
		}
	}

	fmt.Println()

	// Regression check hints
	if passed > 0 {
		fmt.Println("  Performance Baseline:")
		for _, result := range r.results {
			if result.Success {
				fmt.Printf("    %s/%s: %.0f msg/s\n", result.BrokerType, result.BrokerName, result.Throughput)
			}
		}
		fmt.Println()
		fmt.Println("  Save these values to detect regressions in future tests.")
	}
	fmt.Println()
}
