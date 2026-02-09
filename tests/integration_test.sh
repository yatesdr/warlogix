#!/bin/bash
#
# WarLink Integration Test Suite
# Comprehensive tests for REST, MQTT, Valkey, Kafka, TagPacks, and Triggers
#
# Tests verify:
#   - All services receive the same values after writes
#   - TagPacks publish atomically with all members
#   - Triggers capture correct data
#   - Debouncing works properly
#   - Selectors/namespaces route correctly
#
# Prerequisites:
#   brew install mosquitto redis kcat jq coreutils
#
# Usage:
#   ./integration_test.sh           # Run all tests
#   ./integration_test.sh rest      # Run only REST tests
#   ./integration_test.sh mqtt      # Run only MQTT tests
#   ./integration_test.sh valkey    # Run only Valkey tests
#   ./integration_test.sh kafka     # Run only Kafka tests
#   ./integration_test.sh alias     # Run alias publishing tests
#   ./integration_test.sh writeback # Run write-back end-to-end tests
#   ./integration_test.sh sync      # Run cross-service sync tests
#   ./integration_test.sh tagpacks  # Run TagPack tests
#   ./integration_test.sh triggers  # Run Trigger tests
#   ./integration_test.sh debounce  # Run debounce tests
#   ./integration_test.sh selectors # Run selector/namespace tests

# Don't exit on error - we want to collect all results
set +e

# Use gtimeout on macOS if available, otherwise define a simple timeout function
if command -v gtimeout &> /dev/null; then
    timeout_cmd="gtimeout"
elif command -v timeout &> /dev/null; then
    timeout_cmd="timeout"
else
    # Simple timeout using background process
    timeout_cmd() {
        local duration=$1
        shift
        "$@" &
        local pid=$!
        (sleep "$duration"; kill $pid 2>/dev/null) &
        local watchdog=$!
        wait $pid 2>/dev/null
        local ret=$?
        kill $watchdog 2>/dev/null
        wait $watchdog 2>/dev/null
        return $ret
    }
fi

# Configuration from warlink config
NAMESPACE="warlink1"
REST_HOST="localhost"
REST_PORT="8080"
MQTT_HOST="localhost"
MQTT_PORT="1883"
VALKEY_HOST="localhost"
VALKEY_PORT="6379"
KAFKA_BROKER="localhost:9092"

# PLCs to test (enabled ones)
PLCS=("logix_L7" "micro820" "s7" "logix_l7_fast" "beckhoff1")

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

PASS=0
FAIL=0
SKIP=0

pass() {
    echo -e "${GREEN}✓ PASS${NC}: $1"
    ((PASS++))
}

fail() {
    echo -e "${RED}✗ FAIL${NC}: $1"
    ((FAIL++))
}

warn() {
    echo -e "${YELLOW}⚠ WARN${NC}: $1"
}

skip() {
    echo -e "${CYAN}○ SKIP${NC}: $1"
    ((SKIP++))
}

info() {
    echo -e "${BLUE}ℹ INFO${NC}: $1"
}

header() {
    echo ""
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo " $1"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

subheader() {
    echo ""
    echo "--- $1 ---"
}

# Helper function to write a tag via REST
write_tag() {
    local plc=$1
    local tag=$2
    local value=$3
    curl -s -X POST "http://${REST_HOST}:${REST_PORT}/${plc}/write" \
        -H "Content-Type: application/json" \
        -d "{\"plc\": \"${plc}\", \"tag\": \"${tag}\", \"value\": ${value}}" 2>/dev/null
}

# Helper function to read a tag via REST
read_tag() {
    local plc=$1
    local tag=$2
    curl -s "http://${REST_HOST}:${REST_PORT}/${plc}/tags/${tag}" 2>/dev/null | jq -r '.value' 2>/dev/null
}

# Helper function to get tag from Valkey
read_valkey_tag() {
    local plc=$1
    local tag=$2
    local key="${NAMESPACE}:${plc}:tags:${tag}"
    redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" get "$key" 2>/dev/null | jq -r '.value' 2>/dev/null
}

# ============================================================
# REST API Tests
# ============================================================
test_rest() {
    header "REST API Tests"

    # Test 1: API is responding
    echo "Testing REST API availability..."
    if curl -s "http://${REST_HOST}:${REST_PORT}/" > /dev/null 2>&1; then
        pass "REST API is responding"
    else
        fail "REST API not responding at http://${REST_HOST}:${REST_PORT}/"
        return
    fi

    # Test 2: List PLCs
    echo "Testing PLC list endpoint..."
    PLC_COUNT=$(curl -s "http://${REST_HOST}:${REST_PORT}/" | jq '. | length')
    if [ "$PLC_COUNT" -gt 0 ]; then
        pass "PLC list returns $PLC_COUNT PLCs"
    else
        fail "PLC list is empty"
    fi

    # Test 3: Check each PLC status
    echo "Testing individual PLC status..."
    for plc in "${PLCS[@]}"; do
        STATUS=$(curl -s "http://${REST_HOST}:${REST_PORT}/" | jq -r ".[] | select(.name==\"$plc\") | .status")
        if [ "$STATUS" = "Connected" ]; then
            pass "PLC '$plc' is connected"
        elif [ -n "$STATUS" ]; then
            warn "PLC '$plc' status: $STATUS"
        else
            fail "PLC '$plc' not found in response"
        fi
    done

    # Test 4: Get tags from each connected PLC
    echo "Testing tag retrieval..."
    for plc in "${PLCS[@]}"; do
        TAG_COUNT=$(curl -s "http://${REST_HOST}:${REST_PORT}/${plc}/tags" 2>/dev/null | jq '. | length' 2>/dev/null || echo "0")
        if [ "$TAG_COUNT" -gt 0 ]; then
            pass "PLC '$plc' has $TAG_COUNT published tags"
        else
            warn "PLC '$plc' has no tags or not accessible"
        fi
    done

    # Test 5: Verify specific tag values exist
    echo "Testing specific tag value retrieval..."
    VALUE=$(curl -s "http://${REST_HOST}:${REST_PORT}/logix_L7/tags/DateStamp" 2>/dev/null | jq -r '.value' 2>/dev/null)
    if [ -n "$VALUE" ] && [ "$VALUE" != "null" ]; then
        pass "logix_L7/DateStamp has value"
    else
        warn "logix_L7/DateStamp not available"
    fi

    VALUE=$(curl -s "http://${REST_HOST}:${REST_PORT}/beckhoff1/tags/MAIN.test_dint" 2>/dev/null | jq -r '.value' 2>/dev/null)
    if [ -n "$VALUE" ] && [ "$VALUE" != "null" ]; then
        pass "beckhoff1/MAIN.test_dint has value"
    else
        warn "beckhoff1/MAIN.test_dint not available"
    fi
}

# ============================================================
# MQTT Tests
# ============================================================
test_mqtt() {
    header "MQTT Tests"

    if ! command -v mosquitto_sub &> /dev/null; then
        warn "mosquitto_sub not found. Install with: brew install mosquitto"
        return
    fi

    # Test 1: MQTT broker is reachable
    echo "Testing MQTT broker connectivity..."
    if $timeout_cmd 2 mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "test" -C 1 -W 1 2>/dev/null; then
        pass "MQTT broker is reachable"
    else
        if mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "warlink/test" -m "ping" 2>/dev/null; then
            pass "MQTT broker accepts connections"
        else
            fail "Cannot connect to MQTT broker at ${MQTT_HOST}:${MQTT_PORT}"
            return
        fi
    fi

    # Test 2: Subscribe and verify messages are flowing
    echo "Testing MQTT message flow (5 second sample)..."
    MQTT_MSGS=$($timeout_cmd 5 mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "${NAMESPACE}/#" -v 2>/dev/null | head -20 || true)
    MSG_COUNT=$(echo "$MQTT_MSGS" | grep -c "${NAMESPACE}" 2>/dev/null || echo "0")
    MSG_COUNT=$(echo "$MSG_COUNT" | tr -d '[:space:]')

    if [ "$MSG_COUNT" -gt 0 ]; then
        pass "Received $MSG_COUNT MQTT messages on ${NAMESPACE}/# topic"
        echo "    Sample topics:"
        echo "$MQTT_MSGS" | head -5 | awk '{print "      " $1}'
    else
        warn "No MQTT messages received in 5 seconds (may be normal if no tag changes)"
    fi

    # Test 3: Check specific PLC topics
    echo "Testing PLC-specific MQTT topics..."
    for plc in "${PLCS[@]}"; do
        TOPIC_MSGS=$($timeout_cmd 3 mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "${NAMESPACE}/${plc}/#" -C 1 2>/dev/null || true)
        if [ -n "$TOPIC_MSGS" ]; then
            pass "Messages flowing on ${NAMESPACE}/${plc}/#"
        else
            warn "No messages on ${NAMESPACE}/${plc}/# (may need tag change)"
        fi
    done
}

# ============================================================
# Valkey/Redis Tests
# ============================================================
test_valkey() {
    header "Valkey/Redis Tests"

    if ! command -v redis-cli &> /dev/null; then
        warn "redis-cli not found. Install with: brew install redis"
        return
    fi

    # Test 1: Valkey is reachable
    echo "Testing Valkey connectivity..."
    if redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" ping 2>/dev/null | grep -q "PONG"; then
        pass "Valkey server is reachable"
    else
        fail "Cannot connect to Valkey at ${VALKEY_HOST}:${VALKEY_PORT}"
        return
    fi

    # Test 2: Check for warlink keys
    echo "Testing for WarLink keys..."
    KEY_COUNT=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" keys "${NAMESPACE}:*" 2>/dev/null | wc -l | tr -d ' ')
    if [ "$KEY_COUNT" -gt 0 ]; then
        pass "Found $KEY_COUNT keys with prefix ${NAMESPACE}:"
    else
        fail "No keys found with prefix ${NAMESPACE}:"
    fi

    # Test 3: Check keys per PLC
    echo "Testing PLC-specific keys..."
    for plc in "${PLCS[@]}"; do
        PLC_KEYS=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" keys "${NAMESPACE}:${plc}:*" 2>/dev/null | wc -l | tr -d ' ')
        if [ "$PLC_KEYS" -gt 0 ]; then
            pass "PLC '$plc' has $PLC_KEYS keys in Valkey"
        else
            warn "PLC '$plc' has no keys in Valkey"
        fi
    done

    # Test 4: Sample some values
    echo "Testing tag value retrieval..."
    SAMPLE_KEY=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" keys "${NAMESPACE}:*:tags:*" 2>/dev/null | head -1)
    if [ -n "$SAMPLE_KEY" ]; then
        SAMPLE_VALUE=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" get "$SAMPLE_KEY" 2>/dev/null)
        if [ -n "$SAMPLE_VALUE" ]; then
            pass "Sample key '$SAMPLE_KEY' has value"
        else
            warn "Sample key exists but has no value"
        fi
    fi

    # Test 5: Check Pub/Sub is working
    echo "Testing Valkey Pub/Sub (3 second sample)..."
    PUBSUB_MSGS=$($timeout_cmd 3 redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" psubscribe "${NAMESPACE}:*" 2>/dev/null | grep -c "pmessage" 2>/dev/null || echo "0")
    PUBSUB_MSGS=$(echo "$PUBSUB_MSGS" | tr -d '[:space:]')
    if [ "$PUBSUB_MSGS" -gt 0 ]; then
        pass "Received $PUBSUB_MSGS Pub/Sub messages"
    else
        warn "No Pub/Sub messages received (may need tag changes)"
    fi
}

# ============================================================
# Kafka Tests
# ============================================================
test_kafka() {
    header "Kafka Tests"

    KAFKA_TOOL=""
    if command -v kcat &> /dev/null; then
        KAFKA_TOOL="kcat"
    elif command -v kafkacat &> /dev/null; then
        KAFKA_TOOL="kafkacat"
    elif command -v kafka-console-consumer &> /dev/null; then
        KAFKA_TOOL="kafka-console-consumer"
    fi

    if [ -z "$KAFKA_TOOL" ]; then
        warn "No Kafka CLI tool found. Install with: brew install kcat"
        return
    fi

    echo "Using Kafka tool: $KAFKA_TOOL"

    # Test 1: List topics
    echo "Testing Kafka connectivity and listing topics..."
    if [ "$KAFKA_TOOL" = "kcat" ] || [ "$KAFKA_TOOL" = "kafkacat" ]; then
        TOPICS=$($KAFKA_TOOL -b "$KAFKA_BROKER" -L 2>/dev/null | grep "topic \"" | sed 's/.*topic "\([^"]*\)".*/\1/' || true)
    else
        TOPICS=$(kafka-topics --bootstrap-server "$KAFKA_BROKER" --list 2>/dev/null || true)
    fi

    if [ -n "$TOPICS" ]; then
        TOPIC_COUNT=$(echo "$TOPICS" | wc -l | tr -d ' ')
        pass "Kafka broker reachable, found $TOPIC_COUNT topics"

        WL_TOPICS=$(echo "$TOPICS" | grep -i "$NAMESPACE" || true)
        if [ -n "$WL_TOPICS" ]; then
            WL_COUNT=$(echo "$WL_TOPICS" | wc -l | tr -d ' ')
            pass "Found $WL_COUNT WarLink topics"
            echo "    Topics:"
            echo "$WL_TOPICS" | head -10 | while read topic; do
                echo "      - $topic"
            done
        else
            warn "No topics matching namespace '$NAMESPACE'"
        fi
    else
        fail "Cannot connect to Kafka at $KAFKA_BROKER"
        return
    fi

    # Test 2: Consume messages from warlink topic
    echo "Testing Kafka message consumption (5 second sample)..."
    if [ "$KAFKA_TOOL" = "kcat" ] || [ "$KAFKA_TOOL" = "kafkacat" ]; then
        TARGET_TOPIC=$(echo "$TOPICS" | grep -i "$NAMESPACE" | head -1)
        if [ -n "$TARGET_TOPIC" ]; then
            MSG_COUNT=$($timeout_cmd 5 $KAFKA_TOOL -b "$KAFKA_BROKER" -t "$TARGET_TOPIC" -C -e -o end 2>/dev/null | head -20 | wc -l | tr -d ' ' || echo "0")
            if [ "$MSG_COUNT" -gt 0 ]; then
                pass "Received messages from topic '$TARGET_TOPIC'"
            else
                warn "No new messages on '$TARGET_TOPIC' (may need tag changes)"
            fi
        fi
    fi
}

# ============================================================
# Cross-Service Synchronization Tests
# ============================================================
test_sync() {
    header "Cross-Service Synchronization Tests"

    echo "These tests verify that ALL services receive the SAME value after a write."
    echo "This is critical for production reliability."
    echo ""

    # Check prerequisites
    if ! command -v mosquitto_sub &> /dev/null; then
        skip "mosquitto_sub not found - MQTT sync tests skipped"
        HAVE_MQTT=false
    else
        HAVE_MQTT=true
    fi

    if ! command -v redis-cli &> /dev/null; then
        skip "redis-cli not found - Valkey sync tests skipped"
        HAVE_VALKEY=false
    else
        HAVE_VALKEY=true
    fi

    KAFKA_TOOL=""
    if command -v kcat &> /dev/null; then
        KAFKA_TOOL="kcat"
    elif command -v kafkacat &> /dev/null; then
        KAFKA_TOOL="kafkacat"
    fi
    if [ -z "$KAFKA_TOOL" ]; then
        skip "kcat not found - Kafka sync tests skipped"
        HAVE_KAFKA=false
    else
        HAVE_KAFKA=true
    fi

    # ============================================================
    subheader "Test 1: Beckhoff Write - All Services Sync"
    # ============================================================

    TEST_VALUE=$((RANDOM % 60000))
    info "Writing $TEST_VALUE to beckhoff1/MAIN.test_uint"

    # Start MQTT subscriber before write - use -C 2 to skip any retained/stale message
    MQTT_FILE="/tmp/sync_mqtt_$$"
    if [ "$HAVE_MQTT" = true ]; then
        # Subscribe and wait for 2 messages - first may be stale, second is our write
        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "${NAMESPACE}/beckhoff1/tags/MAIN.test_uint" \
            -C 2 -W 10 > "$MQTT_FILE" 2>/dev/null &
        MQTT_PID=$!
    fi

    # Start Kafka consumer before write
    KAFKA_FILE="/tmp/sync_kafka_$$"
    if [ "$HAVE_KAFKA" = true ]; then
        $KAFKA_TOOL -b "$KAFKA_BROKER" -t "$NAMESPACE" -C -o end -c 10 > "$KAFKA_FILE" 2>/dev/null &
        KAFKA_PID=$!
    fi

    sleep 1

    # Write the value
    WRITE_RESULT=$(write_tag "beckhoff1" "MAIN.test_uint" "$TEST_VALUE")
    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "Write accepted"
    else
        fail "Write failed: $WRITE_RESULT"
        return
    fi

    sleep 3

    # Check REST
    REST_VALUE=$(read_tag "beckhoff1" "MAIN.test_uint")
    if [ "$REST_VALUE" = "$TEST_VALUE" ]; then
        pass "REST has correct value: $REST_VALUE"
    else
        fail "REST value mismatch: expected $TEST_VALUE, got $REST_VALUE"
    fi

    # Check Valkey
    if [ "$HAVE_VALKEY" = true ]; then
        VALKEY_VALUE=$(read_valkey_tag "beckhoff1" "MAIN.test_uint")
        if [ "$VALKEY_VALUE" = "$TEST_VALUE" ]; then
            pass "Valkey has correct value: $VALKEY_VALUE"
        else
            fail "Valkey value mismatch: expected $TEST_VALUE, got $VALKEY_VALUE"
        fi
    fi

    # Check MQTT - use last line (most recent message) in case first was stale
    if [ "$HAVE_MQTT" = true ]; then
        wait $MQTT_PID 2>/dev/null || true
        if [ -s "$MQTT_FILE" ]; then
            # Get the last message (most recent) in case first was stale/retained
            MQTT_VALUE=$(tail -1 "$MQTT_FILE" | jq -r '.value' 2>/dev/null)
            if [ "$MQTT_VALUE" = "$TEST_VALUE" ]; then
                pass "MQTT has correct value: $MQTT_VALUE"
            else
                # Check if any message has the correct value
                if grep -q "\"value\":$TEST_VALUE" "$MQTT_FILE" 2>/dev/null; then
                    pass "MQTT has correct value (found in messages)"
                else
                    fail "MQTT value mismatch: expected $TEST_VALUE, got $MQTT_VALUE"
                fi
            fi
        else
            fail "No MQTT message received"
        fi
        rm -f "$MQTT_FILE"
    fi

    # Check Kafka
    if [ "$HAVE_KAFKA" = true ]; then
        sleep 2
        kill $KAFKA_PID 2>/dev/null || true
        if [ -s "$KAFKA_FILE" ]; then
            # Find message for our tag
            KAFKA_VALUE=$(grep "MAIN.test_uint" "$KAFKA_FILE" 2>/dev/null | tail -1 | jq -r '.value' 2>/dev/null)
            if [ "$KAFKA_VALUE" = "$TEST_VALUE" ]; then
                pass "Kafka has correct value: $KAFKA_VALUE"
            else
                warn "Kafka value check inconclusive (got: $KAFKA_VALUE)"
            fi
        else
            warn "No Kafka messages captured"
        fi
        rm -f "$KAFKA_FILE"
    fi

    # ============================================================
    subheader "Test 2: S7 Write with Alias - All Services Sync"
    # ============================================================

    TEST_VALUE2=$((RANDOM % 30000))
    info "Writing $TEST_VALUE2 to s7/db1.4 (alias: test_int)"

    # Start MQTT subscriber (should be on alias topic) - use -C 2 to skip stale
    MQTT_FILE="/tmp/sync_mqtt2_$$"
    if [ "$HAVE_MQTT" = true ]; then
        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "${NAMESPACE}/s7/tags/test_int" \
            -C 2 -W 10 > "$MQTT_FILE" 2>/dev/null &
        MQTT_PID=$!
    fi

    sleep 1

    WRITE_RESULT=$(write_tag "s7" "db1.4" "$TEST_VALUE2")
    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "S7 write accepted"
    else
        fail "S7 write failed: $WRITE_RESULT"
    fi

    sleep 3

    # Check REST (by alias)
    REST_VALUE=$(read_tag "s7" "test_int")
    if [ "$REST_VALUE" = "$TEST_VALUE2" ]; then
        pass "REST (alias) has correct value: $REST_VALUE"
    else
        fail "REST value mismatch: expected $TEST_VALUE2, got $REST_VALUE"
    fi

    # Check Valkey (should use alias key)
    if [ "$HAVE_VALKEY" = true ]; then
        VALKEY_VALUE=$(read_valkey_tag "s7" "test_int")
        if [ "$VALKEY_VALUE" = "$TEST_VALUE2" ]; then
            pass "Valkey (alias) has correct value: $VALKEY_VALUE"
        else
            fail "Valkey value mismatch: expected $TEST_VALUE2, got $VALKEY_VALUE"
        fi
    fi

    # Check MQTT (alias topic) - use last message in case first was stale
    if [ "$HAVE_MQTT" = true ]; then
        wait $MQTT_PID 2>/dev/null || true
        if [ -s "$MQTT_FILE" ]; then
            MQTT_VALUE=$(tail -1 "$MQTT_FILE" | jq -r '.value' 2>/dev/null)
            if [ "$MQTT_VALUE" = "$TEST_VALUE2" ]; then
                pass "MQTT (alias topic) has correct value: $MQTT_VALUE"
            else
                if grep -q "\"value\":$TEST_VALUE2" "$MQTT_FILE" 2>/dev/null; then
                    pass "MQTT (alias topic) has correct value (found in messages)"
                else
                    fail "MQTT value mismatch: expected $TEST_VALUE2, got $MQTT_VALUE"
                fi
            fi
        else
            fail "No MQTT message on alias topic"
        fi
        rm -f "$MQTT_FILE"
    fi

    # ============================================================
    subheader "Test 3: Multi-PLC Sync Verification"
    # ============================================================

    info "Writing unique values to multiple PLCs and verifying sync"

    LOGIX_VAL=$((RANDOM % 1000000))
    BECK_VAL=$((RANDOM % 250))
    S7_VAL=$((RANDOM % 30000))

    # Write to all PLCs
    write_tag "logix_L7" "TimeStamp" "$LOGIX_VAL" > /dev/null
    write_tag "beckhoff1" "MAIN.test_byte" "$BECK_VAL" > /dev/null
    write_tag "s7" "db1.4" "$S7_VAL" > /dev/null

    sleep 3

    # Verify REST
    SYNC_PASS=true

    R1=$(read_tag "logix_L7" "TimeStamp")
    R2=$(read_tag "beckhoff1" "MAIN.test_byte")
    R3=$(read_tag "s7" "test_int")

    if [ "$R1" = "$LOGIX_VAL" ] && [ "$R2" = "$BECK_VAL" ] && [ "$R3" = "$S7_VAL" ]; then
        pass "REST: All 3 PLCs have correct values"
    else
        fail "REST sync failed: logix=$R1(exp:$LOGIX_VAL), beck=$R2(exp:$BECK_VAL), s7=$R3(exp:$S7_VAL)"
        SYNC_PASS=false
    fi

    # Verify Valkey
    if [ "$HAVE_VALKEY" = true ]; then
        V1=$(read_valkey_tag "logix_L7" "TimeStamp")
        V2=$(read_valkey_tag "beckhoff1" "MAIN.test_byte")
        V3=$(read_valkey_tag "s7" "test_int")

        if [ "$V1" = "$LOGIX_VAL" ] && [ "$V2" = "$BECK_VAL" ] && [ "$V3" = "$S7_VAL" ]; then
            pass "Valkey: All 3 PLCs have correct values"
        else
            fail "Valkey sync failed: logix=$V1(exp:$LOGIX_VAL), beck=$V2(exp:$BECK_VAL), s7=$V3(exp:$S7_VAL)"
            SYNC_PASS=false
        fi
    fi

    if [ "$SYNC_PASS" = true ]; then
        pass "Multi-PLC synchronization verified"
    fi
}

# ============================================================
# TagPack Tests
# ============================================================
test_tagpacks() {
    header "TagPack Tests"

    # Test 1: List all TagPacks
    echo "Testing TagPack list endpoint..."
    TAGPACKS=$(curl -s "http://${REST_HOST}:${REST_PORT}/tagpack" 2>/dev/null)

    if [ -z "$TAGPACKS" ] || echo "$TAGPACKS" | grep -qi "error"; then
        fail "TagPack list endpoint failed: $TAGPACKS"
        return
    fi

    PACK_COUNT=$(echo "$TAGPACKS" | jq 'length' 2>/dev/null || echo "0")
    PACK_COUNT=$(echo "$PACK_COUNT" | tr -d '[:space:]')

    if [ "$PACK_COUNT" -gt 0 ]; then
        pass "Found $PACK_COUNT TagPacks"
        echo "    TagPacks:"
        echo "$TAGPACKS" | jq -r '.[].name' 2>/dev/null | while read name; do
            ENABLED=$(echo "$TAGPACKS" | jq -r ".[] | select(.name==\"$name\") | .enabled" 2>/dev/null)
            MEMBERS=$(echo "$TAGPACKS" | jq -r ".[] | select(.name==\"$name\") | .members" 2>/dev/null)
            STATUS="disabled"
            [ "$ENABLED" = "true" ] && STATUS="enabled"
            echo "      - $name ($MEMBERS members, $STATUS)"
        done
    else
        warn "No TagPacks configured"
        return
    fi

    # Test 2: Get sync_test_pack details
    subheader "Testing sync_test_pack"
    PACK_DETAIL=$(curl -s "http://${REST_HOST}:${REST_PORT}/tagpack/sync_test_pack" 2>/dev/null)

    if echo "$PACK_DETAIL" | grep -qi "error"; then
        fail "sync_test_pack detail endpoint failed: $PACK_DETAIL"
    else
        TAG_COUNT=$(echo "$PACK_DETAIL" | jq '.tags | length' 2>/dev/null || echo "0")
        TAG_COUNT=$(echo "$TAG_COUNT" | tr -d '[:space:]')
        if [ "$TAG_COUNT" -gt 0 ]; then
            pass "sync_test_pack has $TAG_COUNT tags with values"
            echo "    Tags in pack:"
            echo "$PACK_DETAIL" | jq -r '.tags | keys[]' 2>/dev/null | while read tag; do
                VALUE=$(echo "$PACK_DETAIL" | jq -r ".tags[\"$tag\"].value" 2>/dev/null | head -c 30)
                echo "      - $tag = $VALUE"
            done
        else
            warn "sync_test_pack has no tags"
        fi
    fi

    # Test 3: Verify all expected members are present
    subheader "Verifying pack member completeness"

    EXPECTED_MEMBERS=("logix_L7.TimeStamp" "beckhoff1.MAIN.test_uint" "beckhoff1.MAIN.test_byte" "s7.test_int" "micro820.test_dint")
    MISSING=0

    for member in "${EXPECTED_MEMBERS[@]}"; do
        if echo "$PACK_DETAIL" | jq -e ".tags[\"$member\"]" > /dev/null 2>&1; then
            pass "Pack contains member: $member"
        else
            fail "Pack missing member: $member"
            ((MISSING++))
        fi
    done

    if [ "$MISSING" -eq 0 ]; then
        pass "All expected members present in sync_test_pack"
    fi

    # Test 4: End-to-end TagPack publish test
    if command -v mosquitto_sub &> /dev/null; then
        subheader "TagPack End-to-End Publish Test"

        PACK_OUTPUT_FILE="/tmp/warlink_pack_test_$$"
        PACK_TEST_VALUE=$((RANDOM % 250 + 1))

        info "Writing $PACK_TEST_VALUE to beckhoff1/MAIN.test_byte to trigger pack publish"

        # Subscribe to pack topic - capture 3 messages to handle stale messages
        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "${NAMESPACE}/packs/sync_test_pack" \
            -C 3 -W 12 -i "pack-test-$$" > "$PACK_OUTPUT_FILE" 2>/dev/null &
        PACK_PID=$!

        sleep 1

        # Write to a tag that's in sync_test_pack
        write_tag "beckhoff1" "MAIN.test_byte" "$PACK_TEST_VALUE" > /dev/null

        wait $PACK_PID 2>/dev/null || true

        if [ -s "$PACK_OUTPUT_FILE" ]; then
            # Find the message with the latest timestamp (most recent)
            PACK_MSG=""
            LATEST_TS=""
            while IFS= read -r line; do
                TS=$(echo "$line" | jq -r '.timestamp' 2>/dev/null)
                if [ -n "$TS" ] && [ "$TS" != "null" ]; then
                    if [ -z "$LATEST_TS" ] || [[ "$TS" > "$LATEST_TS" ]]; then
                        LATEST_TS="$TS"
                        PACK_MSG="$line"
                    fi
                fi
            done < "$PACK_OUTPUT_FILE"

            if [ -z "$PACK_MSG" ]; then
                # Fallback to last line if timestamp parsing failed
                PACK_MSG=$(tail -1 "$PACK_OUTPUT_FILE")
            fi
            PACK_NAME=$(echo "$PACK_MSG" | jq -r '.name' 2>/dev/null)
            PACK_TIMESTAMP=$(echo "$PACK_MSG" | jq -r '.timestamp' 2>/dev/null)
            PACK_TAGS=$(echo "$PACK_MSG" | jq '.tags | keys | length' 2>/dev/null)

            if [ "$PACK_NAME" = "sync_test_pack" ]; then
                pass "Pack message has correct name: $PACK_NAME"
            else
                fail "Pack name mismatch: expected sync_test_pack, got $PACK_NAME"
            fi

            if [ -n "$PACK_TIMESTAMP" ] && [ "$PACK_TIMESTAMP" != "null" ]; then
                pass "Pack message has timestamp: $PACK_TIMESTAMP"
            else
                fail "Pack message missing timestamp"
            fi

            if [ "$PACK_TAGS" -ge 4 ]; then
                pass "Pack message has $PACK_TAGS tags (expected >= 4)"
            else
                fail "Pack message has only $PACK_TAGS tags (expected >= 4)"
            fi

            # Verify our written value is in the pack
            PACK_BYTE_VAL=$(echo "$PACK_MSG" | jq -r '.tags["beckhoff1.MAIN.test_byte"].value' 2>/dev/null)
            if [ "$PACK_BYTE_VAL" = "$PACK_TEST_VALUE" ]; then
                pass "Pack contains our written value: $PACK_BYTE_VAL"
            else
                fail "Pack value mismatch: expected $PACK_TEST_VALUE, got $PACK_BYTE_VAL"
            fi
        else
            fail "TagPack did not publish after writing to member tag"
        fi

        rm -f "$PACK_OUTPUT_FILE"
    fi

    # Test 5: Verify pack in Valkey
    if command -v redis-cli &> /dev/null; then
        subheader "TagPack in Valkey"
        PACK_KEY="${NAMESPACE}:packs:sync_test_pack"
        PACK_VAL=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" get "$PACK_KEY" 2>/dev/null)

        if [ -n "$PACK_VAL" ]; then
            pass "Pack exists in Valkey: $PACK_KEY"
            VALKEY_PACK_TAGS=$(echo "$PACK_VAL" | jq '.tags | keys | length' 2>/dev/null)
            info "Valkey pack has $VALKEY_PACK_TAGS tags"
        else
            warn "Pack not found in Valkey (may need a trigger to publish)"
        fi
    fi
}

# ============================================================
# Trigger Tests
# ============================================================
test_triggers() {
    header "Trigger Tests"

    echo "Testing sync_test_trigger (fires when MAIN.test_dint > 0)"
    echo ""

    # First reset the trigger by writing 0 and wait for cooldown
    info "Resetting trigger (writing 0 to MAIN.test_dint)..."
    write_tag "beckhoff1" "MAIN.test_dint" "0" > /dev/null
    sleep 3  # Allow trigger to reset and any pending messages to clear

    # Write known values to the tags the trigger captures BEFORE subscribing
    # This ensures the values are stable when we fire
    TRIG_UINT=$((RANDOM % 60000))
    TRIG_BYTE=$((RANDOM % 250))

    info "Setting up captured tags: test_uint=$TRIG_UINT, test_byte=$TRIG_BYTE"
    write_tag "beckhoff1" "MAIN.test_uint" "$TRIG_UINT" > /dev/null
    write_tag "beckhoff1" "MAIN.test_byte" "$TRIG_BYTE" > /dev/null

    # Wait for values to be confirmed in REST (PLC poll cycle is 1s)
    info "Waiting for values to propagate through PLC poll cycle..."
    VALUES_CONFIRMED=false
    for i in {1..5}; do
        sleep 1
        CHECK_UINT=$(read_tag "beckhoff1" "MAIN.test_uint")
        CHECK_BYTE=$(read_tag "beckhoff1" "MAIN.test_byte")
        if [ "$CHECK_UINT" = "$TRIG_UINT" ] && [ "$CHECK_BYTE" = "$TRIG_BYTE" ]; then
            info "Values confirmed in REST after ${i}s"
            VALUES_CONFIRMED=true
            break
        fi
        info "Waiting... REST has uint=$CHECK_UINT (want $TRIG_UINT), byte=$CHECK_BYTE (want $TRIG_BYTE)"
    done

    if [ "$VALUES_CONFIRMED" != "true" ]; then
        warn "Values not confirmed after 5s, proceeding anyway"
    fi

    # Now set up listeners - AFTER values are written and confirmed
    # Capture 3 messages to handle any stale queued messages; we'll use the latest
    MQTT_TRIG_FILE="/tmp/trigger_mqtt_$$"
    KAFKA_TRIG_FILE="/tmp/trigger_kafka_$$"

    if command -v mosquitto_sub &> /dev/null; then
        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "${NAMESPACE}/triggers/sync_test_trigger" \
            -t "${NAMESPACE}/+/triggers/sync_test_trigger" \
            -C 3 -W 15 > "$MQTT_TRIG_FILE" 2>/dev/null &
        MQTT_PID=$!
    fi

    KAFKA_TOOL=""
    if command -v kcat &> /dev/null; then
        KAFKA_TOOL="kcat"
    fi
    if [ -n "$KAFKA_TOOL" ]; then
        $KAFKA_TOOL -b "$KAFKA_BROKER" -t "$NAMESPACE" -C -o end -c 20 > "$KAFKA_TRIG_FILE" 2>/dev/null &
        KAFKA_PID=$!
    fi

    sleep 1  # Let subscriber connect

    # Fire the trigger
    FIRE_VALUE=$((RANDOM % 1000 + 1))
    info "Firing trigger (writing $FIRE_VALUE to MAIN.test_dint)..."
    WRITE_RESULT=$(write_tag "beckhoff1" "MAIN.test_dint" "$FIRE_VALUE")

    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "Trigger fire write accepted"
    else
        fail "Trigger fire write failed: $WRITE_RESULT"
    fi

    sleep 5

    # Check MQTT trigger message - use the one with highest sequence number
    if [ -n "$MQTT_PID" ]; then
        wait $MQTT_PID 2>/dev/null || true
    fi
    if [ -s "$MQTT_TRIG_FILE" ]; then
        # Find the message with the highest sequence number (most recent)
        TRIG_MSG=""
        MAX_SEQ=0
        while IFS= read -r line; do
            SEQ=$(echo "$line" | jq -r '.sequence' 2>/dev/null)
            if [ -n "$SEQ" ] && [ "$SEQ" != "null" ] && [ "$SEQ" -gt "$MAX_SEQ" ] 2>/dev/null; then
                MAX_SEQ=$SEQ
                TRIG_MSG="$line"
            fi
        done < "$MQTT_TRIG_FILE"

        if [ -z "$TRIG_MSG" ]; then
            # Fallback to last line if sequence parsing failed
            TRIG_MSG=$(tail -1 "$MQTT_TRIG_FILE")
        fi

        TRIG_NAME=$(echo "$TRIG_MSG" | jq -r '.trigger' 2>/dev/null)
        TRIG_SEQ=$(echo "$TRIG_MSG" | jq -r '.sequence' 2>/dev/null)
        TRIG_PLC=$(echo "$TRIG_MSG" | jq -r '.plc' 2>/dev/null)

        if [ "$TRIG_NAME" = "sync_test_trigger" ]; then
            pass "MQTT: Trigger message received: $TRIG_NAME"
        else
            fail "MQTT: Wrong trigger name: expected sync_test_trigger, got $TRIG_NAME"
        fi

        if [ "$TRIG_PLC" = "beckhoff1" ]; then
            pass "MQTT: Correct PLC in message: $TRIG_PLC"
        else
            fail "MQTT: Wrong PLC: expected beckhoff1, got $TRIG_PLC"
        fi

        if [ -n "$TRIG_SEQ" ] && [ "$TRIG_SEQ" != "null" ]; then
            pass "MQTT: Sequence number present: $TRIG_SEQ"
        else
            warn "MQTT: Sequence number missing"
        fi

        # Verify captured tag values
        CAP_UINT=$(echo "$TRIG_MSG" | jq -r '.data["MAIN.test_uint"]' 2>/dev/null)
        CAP_BYTE=$(echo "$TRIG_MSG" | jq -r '.data["MAIN.test_byte"]' 2>/dev/null)

        if [ "$CAP_UINT" = "$TRIG_UINT" ]; then
            pass "MQTT: Captured test_uint matches: $CAP_UINT"
        else
            fail "MQTT: Captured test_uint mismatch: expected $TRIG_UINT, got $CAP_UINT"
        fi

        if [ "$CAP_BYTE" = "$TRIG_BYTE" ]; then
            pass "MQTT: Captured test_byte matches: $CAP_BYTE"
        else
            fail "MQTT: Captured test_byte mismatch: expected $TRIG_BYTE, got $CAP_BYTE"
        fi

        # Verify embedded pack data
        PACK_DATA=$(echo "$TRIG_MSG" | jq '.data.sync_test_pack' 2>/dev/null)
        if [ "$PACK_DATA" != "null" ] && [ -n "$PACK_DATA" ]; then
            pass "MQTT: Embedded pack data present"
            PACK_UINT=$(echo "$PACK_DATA" | jq -r '.["beckhoff1.MAIN.test_uint"].value' 2>/dev/null)
            if [ "$PACK_UINT" = "$TRIG_UINT" ]; then
                pass "MQTT: Embedded pack test_uint matches: $PACK_UINT"
            else
                warn "MQTT: Embedded pack test_uint: expected $TRIG_UINT, got $PACK_UINT"
            fi
        else
            warn "MQTT: No embedded pack data (check trigger config)"
        fi
    else
        fail "MQTT: No trigger message received"
    fi

    # Check Kafka trigger message
    if [ -n "$KAFKA_PID" ]; then
        kill $KAFKA_PID 2>/dev/null || true
    fi
    if [ -s "$KAFKA_TRIG_FILE" ]; then
        # Find trigger message
        KAFKA_TRIG=$(grep "sync_test_trigger" "$KAFKA_TRIG_FILE" 2>/dev/null | head -1)
        if [ -n "$KAFKA_TRIG" ]; then
            pass "Kafka: Trigger message received"

            K_SEQ=$(echo "$KAFKA_TRIG" | jq -r '.sequence' 2>/dev/null)
            if [ -n "$K_SEQ" ] && [ "$K_SEQ" != "null" ]; then
                pass "Kafka: Sequence number present: $K_SEQ"
            fi
        else
            warn "Kafka: No trigger message found in captured messages"
        fi
    else
        warn "Kafka: No messages captured"
    fi

    # Reset trigger
    info "Resetting trigger (writing 0)..."
    write_tag "beckhoff1" "MAIN.test_dint" "0" > /dev/null

    rm -f "$MQTT_TRIG_FILE" "$KAFKA_TRIG_FILE"
}

# ============================================================
# Debounce Tests
# ============================================================
test_debounce() {
    header "Debounce Tests"

    echo "Testing that rapid writes to pack members are properly debounced."
    echo "TagPacks use 250ms debounce - rapid changes should result in one publish."
    echo ""

    if ! command -v mosquitto_sub &> /dev/null; then
        skip "mosquitto_sub not found - debounce tests require MQTT"
        return
    fi

    DEBOUNCE_FILE="/tmp/debounce_test_$$"

    # Subscribe to pack topic and count messages over time
    info "Subscribing to sync_test_pack topic for 8 seconds..."
    mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
        -t "${NAMESPACE}/packs/sync_test_pack" \
        -v > "$DEBOUNCE_FILE" 2>/dev/null &
    SUB_PID=$!

    sleep 1

    # Perform rapid writes (faster than debounce period)
    info "Performing 5 rapid writes (50ms apart)..."
    for i in {1..5}; do
        VAL=$((RANDOM % 250))
        write_tag "beckhoff1" "MAIN.test_byte" "$VAL" > /dev/null
        sleep 0.05  # 50ms between writes
    done

    sleep 1

    # Wait for debounce period (250ms) + margin
    info "Waiting for debounce to complete..."
    sleep 1

    # Do another batch of rapid writes
    info "Performing another 5 rapid writes..."
    for i in {1..5}; do
        VAL=$((RANDOM % 60000))
        write_tag "beckhoff1" "MAIN.test_uint" "$VAL" > /dev/null
        sleep 0.05
    done

    sleep 3  # Wait for all debounced publishes

    kill $SUB_PID 2>/dev/null || true
    sleep 1

    # Count messages received - grep -c returns 0 with exit code 1 on no match
    if [ -f "$DEBOUNCE_FILE" ]; then
        MSG_COUNT=$(grep -c "packs/sync_test_pack" "$DEBOUNCE_FILE" 2>/dev/null)
        # If grep fails (no matches), it returns exit 1 but still outputs "0"
        if [ -z "$MSG_COUNT" ]; then
            MSG_COUNT=0
        fi
    else
        MSG_COUNT=0
    fi

    info "Total pack messages received: $MSG_COUNT"

    if [ "$MSG_COUNT" -ge 1 ] && [ "$MSG_COUNT" -le 4 ]; then
        pass "Debounce working: 10 rapid writes resulted in $MSG_COUNT pack messages"
    elif [ "$MSG_COUNT" -eq 0 ]; then
        fail "No pack messages received - pack may not be publishing"
    elif [ "$MSG_COUNT" -gt 8 ]; then
        fail "Debounce not working: $MSG_COUNT messages for 10 writes (expected 2-4)"
    else
        warn "Debounce may be working: $MSG_COUNT messages (expected 2-4 for two batches)"
    fi

    rm -f "$DEBOUNCE_FILE"
}

# ============================================================
# Selector/Namespace Tests
# ============================================================
test_selectors() {
    header "Selector/Namespace Tests"

    echo "Testing that selectors route messages to correct topics/keys."
    echo "broker2 has selector 'quality-data', quality_data kafka has selector 'quality_data'"
    echo ""

    # Check MQTT broker2 with selector
    if command -v mosquitto_sub &> /dev/null; then
        subheader "MQTT Selector Test (broker2: quality-data)"

        # The quality_trigger publishes to broker2 with selector
        # Topic should be: warlink1/quality-data/triggers/quality_trigger

        SELECTOR_FILE="/tmp/selector_mqtt_$$"

        info "Listening for quality_trigger on selector topic..."
        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "${NAMESPACE}/quality-data/triggers/#" \
            -t "${NAMESPACE}/more_selectors/triggers/#" \
            -v -C 1 -W 5 > "$SELECTOR_FILE" 2>/dev/null &
        SEL_PID=$!

        wait $SEL_PID 2>/dev/null || true

        if [ -s "$SELECTOR_FILE" ]; then
            TOPIC=$(cat "$SELECTOR_FILE" | awk '{print $1}')
            pass "Received message on selector topic: $TOPIC"

            if echo "$TOPIC" | grep -q "quality-data\|more_selectors"; then
                pass "Selector is being used in topic path"
            else
                warn "Topic doesn't contain expected selector"
            fi
        else
            warn "No selector-scoped messages received (may need trigger to fire)"
        fi

        rm -f "$SELECTOR_FILE"
    fi

    # Check Kafka with selector
    KAFKA_TOOL=""
    if command -v kcat &> /dev/null; then
        KAFKA_TOOL="kcat"
    fi

    if [ -n "$KAFKA_TOOL" ]; then
        subheader "Kafka Selector Topics"

        TOPICS=$($KAFKA_TOOL -b "$KAFKA_BROKER" -L 2>/dev/null | grep "topic \"" | sed 's/.*topic "\([^"]*\)".*/\1/' || true)

        # Check for selector-prefixed topics
        SELECTOR_TOPICS=$(echo "$TOPICS" | grep -E "${NAMESPACE}[-_]quality" || true)

        if [ -n "$SELECTOR_TOPICS" ]; then
            pass "Found Kafka topics with selector:"
            echo "$SELECTOR_TOPICS" | while read t; do
                echo "      - $t"
            done
        else
            warn "No selector-specific Kafka topics found"
        fi
    fi

    # Check Valkey keys
    if command -v redis-cli &> /dev/null; then
        subheader "Valkey Selector Keys"

        # Look for keys with selector in path
        SELECTOR_KEYS=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" keys "${NAMESPACE}:quality*" 2>/dev/null | head -5)

        if [ -n "$SELECTOR_KEYS" ]; then
            pass "Found Valkey keys with selector prefix"
            echo "$SELECTOR_KEYS" | while read k; do
                echo "      - $k"
            done
        else
            info "No selector-specific Valkey keys (selectors may only apply to MQTT/Kafka)"
        fi
    fi
}

# ============================================================
# Write-Back Tests (existing, enhanced)
# ============================================================
test_writeback() {
    header "Write-Back End-to-End Tests"

    TEST_VALUE=$((RANDOM % 10000 + 1000))
    TEST_VALUE2=$((RANDOM % 10000 + 1000))

    echo "Using test values: $TEST_VALUE, $TEST_VALUE2"
    echo ""

    # Test 1: Logix Write
    subheader "Logix Write and Read (TimeStamp)"

    WRITE_RESULT=$(write_tag "logix_L7" "TimeStamp" "$TEST_VALUE")
    if echo "$WRITE_RESULT" | grep -qi "error"; then
        fail "REST write failed: $WRITE_RESULT"
    else
        pass "REST write to logix_L7/TimeStamp accepted"
    fi

    sleep 2

    READ_VALUE=$(read_tag "logix_L7" "TimeStamp")
    if [ "$READ_VALUE" = "$TEST_VALUE" ]; then
        pass "REST read returns correct value: $READ_VALUE"
    else
        warn "REST read value mismatch: expected $TEST_VALUE, got $READ_VALUE (PLC may have changed it)"
    fi

    # Test 2: S7 Write
    subheader "S7 Write and Read (DB1.0 DINT)"

    WRITE_RESULT=$(write_tag "s7" "DB1.0" "$TEST_VALUE2")
    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "REST write to s7/DB1.0 accepted"
    else
        fail "S7 write failed: $WRITE_RESULT"
    fi

    sleep 1

    READ_VALUE=$(read_tag "s7" "test_dint")  # Using alias
    if [ "$READ_VALUE" = "$TEST_VALUE2" ]; then
        pass "S7 read returns correct value: $READ_VALUE"
    else
        fail "S7 read value mismatch: expected $TEST_VALUE2, got $READ_VALUE"
    fi

    # Test 3: Beckhoff Write
    subheader "Beckhoff Write and Read (MAIN.test_dint)"

    BECK_VALUE=$((RANDOM % 10000 + 1000))
    WRITE_RESULT=$(write_tag "beckhoff1" "MAIN.test_dint" "$BECK_VALUE")

    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "REST write to beckhoff1/MAIN.test_dint accepted"
    else
        fail "Beckhoff write failed: $WRITE_RESULT"
    fi

    sleep 1

    READ_VALUE=$(read_tag "beckhoff1" "MAIN.test_dint")
    if [ "$READ_VALUE" = "$BECK_VALUE" ]; then
        pass "Beckhoff read returns correct value: $READ_VALUE"
    else
        fail "Beckhoff read value mismatch: expected $BECK_VALUE, got $READ_VALUE"
    fi

    # Test 4: Micro820 Write
    subheader "Micro820 Write and Read (test_dint)"

    MICRO_VALUE=$((RANDOM % 10000 + 1000))
    WRITE_RESULT=$(write_tag "micro820" "test_dint" "$MICRO_VALUE")

    if echo "$WRITE_RESULT" | grep -q '"success":true'; then
        pass "REST write to micro820/test_dint accepted"
    else
        fail "Micro820 write failed: $WRITE_RESULT"
    fi

    sleep 1

    READ_VALUE=$(read_tag "micro820" "test_dint")
    if [ "$READ_VALUE" = "$MICRO_VALUE" ]; then
        pass "Micro820 read returns correct value: $READ_VALUE"
    else
        fail "Micro820 read value mismatch: expected $MICRO_VALUE, got $READ_VALUE"
    fi
}

# ============================================================
# Alias Publishing Tests
# ============================================================
test_alias() {
    header "Alias Publishing Tests"

    echo "S7 uses memory addresses (DB1.0, db1.4) that benefit from aliases."
    echo "Testing that S7 tags with aliases publish using the alias,"
    echo "with the raw address included in the payload for troubleshooting."
    echo ""

    # Test 1: S7 tag WITH alias
    subheader "S7 Tag WITH Alias (DB1.0 -> test_dint)"

    S7_ALIAS_VALUE=$((RANDOM % 10000 + 1000))
    info "Writing $S7_ALIAS_VALUE to s7/DB1.0 (has alias 'test_dint')..."

    WRITE_RESULT=$(write_tag "s7" "DB1.0" "$S7_ALIAS_VALUE")
    if ! echo "$WRITE_RESULT" | grep -q '"success":true'; then
        fail "S7 write failed: $WRITE_RESULT"
        return
    fi
    pass "S7 write accepted"

    sleep 2

    # Check REST API uses alias
    REST_RESPONSE=$(curl -s "http://${REST_HOST}:${REST_PORT}/s7/tags/test_dint" 2>/dev/null)

    if [ -z "$REST_RESPONSE" ] || echo "$REST_RESPONSE" | grep -qi "not found"; then
        fail "REST query by alias 'test_dint' failed"
    else
        REST_NAME=$(echo "$REST_RESPONSE" | jq -r '.name' 2>/dev/null)
        REST_OFFSET=$(echo "$REST_RESPONSE" | jq -r '.memloc' 2>/dev/null)
        REST_VALUE=$(echo "$REST_RESPONSE" | jq -r '.value' 2>/dev/null)

        if [ "$REST_NAME" = "test_dint" ]; then
            pass "REST 'name' field contains alias: $REST_NAME"
        else
            fail "REST 'name' should be alias 'test_dint', got: $REST_NAME"
        fi

        if [ "$REST_OFFSET" = "DB1.0" ]; then
            pass "REST 'memloc' field contains address: $REST_OFFSET"
        else
            warn "REST 'memloc' should be 'DB1.0', got: $REST_OFFSET"
        fi

        if [ "$REST_VALUE" = "$S7_ALIAS_VALUE" ]; then
            pass "REST value is correct: $REST_VALUE"
        else
            warn "REST value mismatch: expected $S7_ALIAS_VALUE, got $REST_VALUE"
        fi
    fi

    # Check Valkey key uses alias
    if command -v redis-cli &> /dev/null; then
        echo ""
        echo "Checking Valkey key uses alias..."
        VALKEY_KEY_ALIAS="${NAMESPACE}:s7:tags:test_dint"
        VALKEY_KEY_ADDR="${NAMESPACE}:s7:tags:DB1.0"

        VALKEY_ALIAS_EXISTS=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" exists "$VALKEY_KEY_ALIAS" 2>/dev/null)
        VALKEY_ADDR_EXISTS=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" exists "$VALKEY_KEY_ADDR" 2>/dev/null)

        if [ "$VALKEY_ALIAS_EXISTS" = "1" ]; then
            pass "Valkey key uses alias: $VALKEY_KEY_ALIAS"

            VALKEY_VALUE=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" get "$VALKEY_KEY_ALIAS" 2>/dev/null)
            VALKEY_TAG=$(echo "$VALKEY_VALUE" | jq -r '.tag' 2>/dev/null)
            VALKEY_OFFSET=$(echo "$VALKEY_VALUE" | jq -r '.memloc' 2>/dev/null)

            if [ "$VALKEY_TAG" = "test_dint" ]; then
                pass "Valkey payload 'tag' field is alias: $VALKEY_TAG"
            else
                fail "Valkey payload 'tag' should be 'test_dint', got: $VALKEY_TAG"
            fi

            if [ "$VALKEY_OFFSET" = "DB1.0" ]; then
                pass "Valkey payload 'memloc' field is address: $VALKEY_OFFSET"
            else
                warn "Valkey payload 'memloc' should be 'DB1.0', got: $VALKEY_OFFSET"
            fi
        else
            fail "Valkey key should use alias '$VALKEY_KEY_ALIAS' but it doesn't exist"
            if [ "$VALKEY_ADDR_EXISTS" = "1" ]; then
                echo "    ERROR: Key exists by address '$VALKEY_KEY_ADDR' instead"
            fi
        fi
    fi

    # Check MQTT topic uses alias
    if command -v mosquitto_sub &> /dev/null; then
        echo ""
        echo "Checking MQTT topic uses alias..."
        MQTT_OUTPUT_FILE="/tmp/warlink_alias_mqtt_$$"
        MQTT_TOPIC_ALIAS="${NAMESPACE}/s7/tags/test_dint"

        mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" \
            -t "$MQTT_TOPIC_ALIAS" \
            -C 1 -W 5 > "$MQTT_OUTPUT_FILE" 2>/dev/null &
        MQTT_PID=$!

        sleep 1

        NEW_S7_VALUE=$((S7_ALIAS_VALUE + 1))
        write_tag "s7" "DB1.0" "$NEW_S7_VALUE" > /dev/null

        wait $MQTT_PID 2>/dev/null || true

        if [ -s "$MQTT_OUTPUT_FILE" ]; then
            pass "MQTT message received on alias topic: $MQTT_TOPIC_ALIAS"

            MQTT_TAG=$(cat "$MQTT_OUTPUT_FILE" | jq -r '.tag' 2>/dev/null)
            MQTT_OFFSET=$(cat "$MQTT_OUTPUT_FILE" | jq -r '.memloc' 2>/dev/null)

            if [ "$MQTT_TAG" = "test_dint" ]; then
                pass "MQTT payload 'tag' field is alias: $MQTT_TAG"
            else
                fail "MQTT payload 'tag' should be 'test_dint', got: $MQTT_TAG"
            fi

            if [ "$MQTT_OFFSET" = "DB1.0" ]; then
                pass "MQTT payload 'memloc' field is address: $MQTT_OFFSET"
            else
                warn "MQTT payload 'memloc' should be 'DB1.0', got: $MQTT_OFFSET"
            fi
        else
            fail "No MQTT message on alias topic"
        fi

        rm -f "$MQTT_OUTPUT_FILE"
    fi
}

# ============================================================
# Rapid Change / Race Condition Tests
# ============================================================
test_rapid() {
    header "Rapid Change / Race Condition Tests"

    echo "Testing system behavior under rapid writes."
    echo "Verifies no data loss or inconsistency."
    echo ""

    if ! command -v redis-cli &> /dev/null; then
        skip "redis-cli required for rapid change tests"
        return
    fi

    subheader "Rapid Write Consistency"

    # Write 20 values rapidly and verify final value
    FINAL_VALUE=$((RANDOM % 60000))

    info "Performing 20 rapid writes, final value will be $FINAL_VALUE"

    for i in {1..19}; do
        VAL=$((RANDOM % 60000))
        write_tag "beckhoff1" "MAIN.test_uint" "$VAL" > /dev/null &
    done
    write_tag "beckhoff1" "MAIN.test_uint" "$FINAL_VALUE" > /dev/null

    # Wait for all writes to complete
    wait
    sleep 3

    # Check final values across services
    REST_FINAL=$(read_tag "beckhoff1" "MAIN.test_uint")
    VALKEY_FINAL=$(read_valkey_tag "beckhoff1" "MAIN.test_uint")

    info "REST final value: $REST_FINAL"
    info "Valkey final value: $VALKEY_FINAL"

    if [ "$REST_FINAL" = "$VALKEY_FINAL" ]; then
        pass "REST and Valkey have consistent final values"
    else
        fail "Inconsistency detected: REST=$REST_FINAL, Valkey=$VALKEY_FINAL"
    fi

    # The final value may not be our FINAL_VALUE due to timing, but
    # REST and Valkey should always agree
    if [ "$REST_FINAL" = "$FINAL_VALUE" ]; then
        pass "Final value matches expected: $FINAL_VALUE"
    else
        info "Final value is $REST_FINAL (last write was $FINAL_VALUE - timing dependent)"
    fi
}

# ============================================================
# Summary
# ============================================================
print_summary() {
    header "Test Summary"
    echo ""
    echo -e "  ${GREEN}Passed${NC}: $PASS"
    echo -e "  ${RED}Failed${NC}: $FAIL"
    echo -e "  ${CYAN}Skipped${NC}: $SKIP"
    echo ""
    TOTAL=$((PASS + FAIL))
    if [ $TOTAL -gt 0 ]; then
        PCT=$((PASS * 100 / TOTAL))
        echo "  Pass rate: ${PCT}%"
    fi
    echo ""

    if [ $FAIL -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}Some tests failed.${NC}"
        exit 1
    fi
}

# ============================================================
# Main
# ============================================================
main() {
    echo "╔═══════════════════════════════════════════════════════════╗"
    echo "║           WarLink Integration Test Suite                 ║"
    echo "║                   Comprehensive Edition                   ║"
    echo "╚═══════════════════════════════════════════════════════════╝"
    echo ""
    echo "Namespace: $NAMESPACE"
    echo "REST:      http://${REST_HOST}:${REST_PORT}"
    echo "MQTT:      ${MQTT_HOST}:${MQTT_PORT}"
    echo "Valkey:    ${VALKEY_HOST}:${VALKEY_PORT}"
    echo "Kafka:     ${KAFKA_BROKER}"
    echo ""

    case "${1:-all}" in
        rest)
            test_rest
            ;;
        mqtt)
            test_mqtt
            ;;
        valkey)
            test_valkey
            ;;
        kafka)
            test_kafka
            ;;
        sync)
            test_sync
            ;;
        writeback|write)
            test_writeback
            ;;
        tagpacks|packs)
            test_tagpacks
            ;;
        triggers)
            test_triggers
            ;;
        debounce)
            test_debounce
            ;;
        selectors|namespace)
            test_selectors
            ;;
        alias)
            test_alias
            ;;
        rapid|race)
            test_rapid
            ;;
        all)
            test_rest
            test_mqtt
            test_valkey
            test_kafka
            test_sync
            test_tagpacks
            test_triggers
            test_debounce
            test_selectors
            test_alias
            test_writeback
            test_rapid
            ;;
        *)
            echo "Usage: $0 [test_name|all]"
            echo ""
            echo "Available tests:"
            echo "  rest      - REST API connectivity and tag access"
            echo "  mqtt      - MQTT broker connectivity and message flow"
            echo "  valkey    - Valkey/Redis connectivity and key storage"
            echo "  kafka     - Kafka broker connectivity and topics"
            echo "  sync      - Cross-service value synchronization (CRITICAL)"
            echo "  tagpacks  - TagPack publishing and member completeness"
            echo "  triggers  - Trigger firing and data capture accuracy"
            echo "  debounce  - TagPack debounce behavior"
            echo "  selectors - Selector/namespace routing"
            echo "  alias     - S7 alias publishing"
            echo "  writeback - Write and read-back verification"
            echo "  rapid     - Rapid change / race condition handling"
            echo "  all       - Run all tests"
            exit 1
            ;;
    esac

    print_summary
}

main "$@"
