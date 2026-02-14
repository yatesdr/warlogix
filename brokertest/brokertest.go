// Package brokertest provides stress testing for message broker connections.
package brokertest

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"warlink/config"
	"warlink/kafka"
	"warlink/mqtt"
	"warlink/valkey"
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
		Selector:         cfg.Selector,
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
	fmt.Printf("  Topic:   warlink-test-stress\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config with test namespace
	testCfg := *cfg
	testCfg.PublishChanges = true
	testCfg.AutoCreateTopics = true

	// Use a test namespace for isolation
	testNamespace := "warlink-test-stress"

	// Use the Manager for batched publishing (matches real-world usage)
	mgr := kafka.NewManager()
	mgr.AddCluster(&testCfg, testNamespace)
	if err := mgr.Connect(testCfg.Name); err != nil {
		result.Error = fmt.Errorf("connect failed: %w", err)
		fmt.Printf("  Status: FAILED - %v\n\n", result.Error)
		return result
	}
	defer mgr.StopAll()

	fmt.Printf("  Running... ")

	// Run the stress test with batched publishing and confirmed delivery tracking
	result = r.runKafkaBatchedStress(mgr, &testCfg, result)

	if result.Success {
		fmt.Printf("DONE\n\n")
	} else {
		fmt.Printf("FAILED\n\n")
	}

	return result
}

// runKafkaBatchedStress executes a Kafka stress test using batched publishing.
// This matches real-world usage and tracks confirmed delivery, not just queue rate.
func (r *Runner) runKafkaBatchedStress(mgr *kafka.Manager, cfg *kafka.Config, result TestResult) TestResult {
	var queued int64

	// Get initial stats
	producer := mgr.GetProducer(cfg.Name)
	if producer == nil {
		result.Error = fmt.Errorf("producer not found for cluster '%s'", cfg.Name)
		return result
	}

	// Verify producer is connected
	status := producer.GetStatus()
	if status != kafka.StatusConnected {
		result.Error = fmt.Errorf("producer not connected (status: %s)", status.String())
		return result
	}

	var initialSent, initialErrors int64
	initialSent, initialErrors, _ = producer.GetStats()

	stopChan := make(chan struct{})
	time.AfterFunc(r.testCfg.Duration, func() { close(stopChan) })

	startTime := time.Now()

	// Simulate PLC tag changes using batched publishing (like real-world)
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

			// Queue to batch processor (non-blocking, like real-world)
			mgr.Publish(plcName, tagName, "", "", "DINT", value, false, true)
			atomic.AddInt64(&queued, 1)
		}
	}

done:
	// Wait for batches to flush completely (batch interval is 20ms, give extra time)
	// Poll until no new messages are being sent
	var lastSent int64
	for i := 0; i < 50; i++ { // Up to 5 seconds
		time.Sleep(100 * time.Millisecond)
		if producer != nil {
			currentSent, _, _ := producer.GetStats()
			if currentSent == lastSent {
				// No new messages sent, batches are flushed
				break
			}
			lastSent = currentSent
		}
	}

	result.Duration = time.Since(startTime)
	result.MessagesSent = queued

	// Get actual confirmed delivery count from producer
	if producer != nil {
		finalSent, finalErrors, _ := producer.GetStats()
		result.MessagesAcked = finalSent - initialSent
		result.Errors = finalErrors - initialErrors
	} else {
		result.MessagesAcked = 0
		result.Errors = queued
	}

	// Throughput based on confirmed deliveries
	result.Throughput = float64(result.MessagesAcked) / result.Duration.Seconds()

	// Success if we delivered messages with low error rate
	// (queue rate may exceed delivery rate, that's expected)
	if result.MessagesAcked > 0 {
		errorRate := float64(result.Errors) / float64(result.MessagesAcked+result.Errors)
		result.Success = errorRate < 0.01 // Less than 1% errors
	} else {
		result.Success = false
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
	fmt.Printf("  Topic:   warlink-test-stress/+/tags/+\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config with test namespace
	testCfg := *cfg
	testCfg.ClientID = fmt.Sprintf("warlink-stress-%d", time.Now().UnixNano())

	// Use a test namespace for isolation
	testNamespace := "warlink-test-stress"

	// Create publisher
	pub := mqtt.NewPublisher(&testCfg, testNamespace)
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

// runMQTTStress executes the MQTT stress test with synchronous publishing.
// This measures actual confirmed delivery, not queue rate.
func (r *Runner) runMQTTStress(pub *mqtt.Publisher, result TestResult) TestResult {
	var sent, errors int64
	latencies := make([]time.Duration, 0, 100000)

	ctx, cancel := context.WithTimeout(context.Background(), r.testCfg.Duration)
	defer cancel()

	startTime := time.Now()

	// Synchronous publish - wait for broker ack on each message
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

			msgStart := time.Now()
			if pub.PublishSync(plcName, tagName, "", "", "DINT", value, false) {
				latency := time.Since(msgStart)
				atomic.AddInt64(&sent, 1)
				latencies = append(latencies, latency)
			} else {
				atomic.AddInt64(&errors, 1)
			}
		}
	}

done:
	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent
	result.Errors = errors
	result.Throughput = float64(sent) / result.Duration.Seconds()
	result.Success = errors == 0 && sent > 0

	// Calculate latency percentiles
	if len(latencies) > 0 {
		result.AvgLatency, result.P50Latency, result.P95Latency, result.P99Latency, result.MaxLatency = calculateLatencyStats(latencies)
	}

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
	fmt.Printf("  Keys:    warlink-test-stress:*\n")
	fmt.Printf("─────────────────────────────────────────────────────────────────────\n")

	// Create a test config
	testCfg := *cfg
	testCfg.PublishChanges = false // Disable pub/sub for pure SET throughput

	// Use a test namespace for isolation
	testNamespace := "warlink-test-stress"

	// Create publisher directly for synchronous testing
	pub := valkey.NewPublisher(&testCfg, testNamespace)
	if err := pub.Start(); err != nil {
		result.Error = fmt.Errorf("connect failed: %w", err)
		fmt.Printf("  Status: FAILED - %v\n\n", result.Error)
		return result
	}
	defer pub.Stop()

	fmt.Printf("  Running... ")

	// Run the stress test with synchronous publishing (measures real throughput)
	result = r.runValkeyStress(pub, result)

	if result.Success {
		fmt.Printf("DONE\n\n")
	} else {
		fmt.Printf("FAILED\n\n")
	}

	return result
}

// runValkeyStress executes the Valkey stress test with synchronous publishing.
// This measures actual Redis throughput, not queue rate.
func (r *Runner) runValkeyStress(pub *valkey.Publisher, result TestResult) TestResult {
	var sent, errors int64
	latencies := make([]time.Duration, 0, 100000)

	ctx, cancel := context.WithTimeout(context.Background(), r.testCfg.Duration)
	defer cancel()

	startTime := time.Now()

	// Generate test messages with synchronous SET operations
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

			msgStart := time.Now()
			err := pub.Publish(plcName, tagName, "", "", "DINT", value, false)
			latency := time.Since(msgStart)

			if err != nil {
				atomic.AddInt64(&errors, 1)
			} else {
				atomic.AddInt64(&sent, 1)
				latencies = append(latencies, latency)
			}
		}
	}

done:
	result.Duration = time.Since(startTime)
	result.MessagesSent = sent
	result.MessagesAcked = sent
	result.Errors = errors
	result.Throughput = float64(sent) / result.Duration.Seconds()
	result.Success = errors == 0 && sent > 0

	// Calculate latency percentiles
	if len(latencies) > 0 {
		result.AvgLatency, result.P50Latency, result.P95Latency, result.P99Latency, result.MaxLatency = calculateLatencyStats(latencies)
	}

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
		fmt.Println("  To run tests, enable brokers in ~/.warlink/config.yaml:")
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

	// Important note about what this test measures
	fmt.Println("─────────────────────────────────────────────────────────────────────")
	fmt.Printf("  TEST CONDITIONS: %d PLCs × %d tags = %d total tags\n",
		r.testCfg.NumPLCs, r.testCfg.NumTags, r.testCfg.NumPLCs*r.testCfg.NumTags)
	fmt.Println()
	fmt.Println("  WHAT THIS MEANS:")
	fmt.Println("  - Throughput shows confirmed message delivery rate per broker")
	fmt.Println("  - With change filtering, real-world rates depend on how often values change")
	fmt.Println("  - Example: 50 PLCs polled at 2Hz with 10% value change rate = 1,000 msg/s")
	fmt.Println()
	fmt.Println("  NOTE: This tests republishing throughput only, not PLC read performance.")
	fmt.Println("  PLC reads may be substantially slower depending on network latency, PLC")
	fmt.Println("  load, and protocol overhead. Use this test to identify broker bottlenecks.")
	fmt.Println()
}
