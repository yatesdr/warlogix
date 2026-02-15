#!/usr/bin/env bash
#
# WarLink Round-Trip Integration Test
#
# Tests the full publish + write-back cycle for every service:
#   1. Write a DINT value via REST API to each PLC
#   2. Verify that value propagated to: REST, MQTT, Valkey, Kafka, Warcry
#   3. Write a different value back through each service and verify the PLC accepted it
#
# For each PLC, the script discovers available DINT tags at runtime and
# probes for one that's writable. If a DINT is blocked by the PLC, it
# retries up to MAX_DINT_ATTEMPTS different DINTs before reporting failure.
#
# Prerequisites:
#   brew install mosquitto redis kcat jq coreutils
#
# Usage:
#   ./roundtrip_test.sh              # Test with warlink already running
#   ./roundtrip_test.sh --start      # Build & start warlink first
#

set +e

# ── Configuration ──────────────────────────────────────────────────────────
NAMESPACE="warlink1"
API_BASE="http://localhost:8080/api"
MQTT_HOST="localhost"
MQTT_PORT="1883"
VALKEY_HOST="127.0.0.1"
VALKEY_PORT="6379"
KAFKA_BROKER="localhost:9092"
WARCRY_HOST="127.0.0.1"
WARCRY_PORT="9999"

MAX_DINT_ATTEMPTS=5
PROPAGATION_WAIT=4   # seconds to wait for a value to propagate
POLL_CYCLE_WAIT=2    # seconds to wait for PLC poll cycle

SCRIPT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LOG_FILE="/tmp/warlink_roundtrip_$(date +%Y%m%d_%H%M%S).log"
WARLINK_PID=""

# Use gtimeout on macOS if available
if command -v gtimeout &>/dev/null; then
    TIMEOUT_CMD="gtimeout"
elif command -v timeout &>/dev/null; then
    TIMEOUT_CMD="timeout"
else
    # Fallback: background + sleep + kill
    TIMEOUT_CMD=""
fi

do_timeout() {
    local secs=$1; shift
    if [ -n "$TIMEOUT_CMD" ]; then
        $TIMEOUT_CMD "$secs" "$@"
    else
        "$@" &
        local pid=$!
        ( sleep "$secs"; kill "$pid" 2>/dev/null ) &
        local wdog=$!
        wait "$pid" 2>/dev/null
        local rc=$?
        kill "$wdog" 2>/dev/null; wait "$wdog" 2>/dev/null
        return $rc
    fi
}

# ── Colors / output ───────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
BLUE='\033[0;34m'; CYAN='\033[0;36m'; NC='\033[0m'
PASS=0; FAIL=0; SKIP=0

log()     { echo "[$(date +%T)] $*" | tee -a "$LOG_FILE"; }
pass()    { ((PASS++)); echo -e "  ${GREEN}PASS${NC}: $*" | tee -a "$LOG_FILE"; }
fail()    { ((FAIL++)); echo -e "  ${RED}FAIL${NC}: $*" | tee -a "$LOG_FILE"; }
skip()    { ((SKIP++)); echo -e "  ${YELLOW}SKIP${NC}: $*" | tee -a "$LOG_FILE"; }
info()    { echo -e "  ${CYAN}INFO${NC}: $*" | tee -a "$LOG_FILE"; }
section() { echo -e "\n${BLUE}=== $* ===${NC}" | tee -a "$LOG_FILE"; }

# ── REST helpers ──────────────────────────────────────────────────────────
api_get() {
    curl -sf --connect-timeout 3 --max-time 10 "${API_BASE}$1" 2>/dev/null
}

api_post() {
    local path=$1 body=$2
    curl -sf --connect-timeout 3 --max-time 10 \
        -X POST -H "Content-Type: application/json" \
        -d "$body" "${API_BASE}${path}" 2>/dev/null
}

# Write a tag via REST.  Returns the full JSON response (including errors).
rest_write() {
    local plc=$1 tag=$2 value=$3
    curl -s --connect-timeout 3 --max-time 10 \
        -X POST -H "Content-Type: application/json" \
        -d "{\"plc\":\"${plc}\",\"tag\":\"${tag}\",\"value\":${value}}" \
        "${API_BASE}/${plc}/write" 2>/dev/null
}

# Read a tag value via REST.  Prints just the numeric value.
rest_read() {
    local plc=$1 tag=$2
    api_get "/${plc}/tags/${tag}" | jq -r '.value' 2>/dev/null
}

# ── Service availability flags ────────────────────────────────────────────
HAVE_MQTT=false; HAVE_VALKEY=false; HAVE_KAFKA=false; HAVE_WARCRY=false

check_services() {
    section "Service Availability"

    # REST / warlink
    if api_get "/" >/dev/null 2>&1; then
        pass "REST API responding at ${API_BASE}"
    else
        fail "REST API not responding -- is warlink running?"
        echo "    Start warlink first, or re-run with --start"
        exit 1
    fi

    # MQTT
    if command -v mosquitto_sub &>/dev/null && command -v mosquitto_pub &>/dev/null; then
        if mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "__probe" -m "1" 2>/dev/null; then
            HAVE_MQTT=true
            pass "MQTT broker reachable (${MQTT_HOST}:${MQTT_PORT})"
        else
            skip "MQTT broker not reachable"
        fi
    else
        skip "mosquitto_pub/sub not installed (brew install mosquitto)"
    fi

    # Valkey
    if command -v redis-cli &>/dev/null; then
        if redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" ping 2>/dev/null | grep -q PONG; then
            HAVE_VALKEY=true
            pass "Valkey reachable (${VALKEY_HOST}:${VALKEY_PORT})"
        else
            skip "Valkey not reachable"
        fi
    else
        skip "redis-cli not installed (brew install redis)"
    fi

    # Kafka
    if command -v kcat &>/dev/null; then
        if kcat -b "$KAFKA_BROKER" -L -t "__consumer_offsets" 2>/dev/null | grep -q "broker"; then
            HAVE_KAFKA=true
            pass "Kafka reachable (${KAFKA_BROKER})"
        else
            skip "Kafka broker not reachable"
        fi
    else
        skip "kcat not installed (brew install kcat)"
    fi

    # Warcry
    if (echo '{"type":"get_config"}' | nc -w2 "$WARCRY_HOST" "$WARCRY_PORT") >/dev/null 2>&1; then
        HAVE_WARCRY=true
        pass "Warcry reachable (${WARCRY_HOST}:${WARCRY_PORT})"
    else
        skip "Warcry not reachable at ${WARCRY_HOST}:${WARCRY_PORT}"
    fi
}

# ── Discover connected PLCs ───────────────────────────────────────────────
declare -a CONNECTED_PLCS

discover_plcs() {
    section "PLC Discovery"
    local raw
    raw=$(api_get "/")
    if [ -z "$raw" ]; then
        fail "Could not list PLCs"; return 1
    fi

    local names statuses
    names=$(echo "$raw" | jq -r '.[].name' 2>/dev/null)
    for name in $names; do
        local status
        status=$(echo "$raw" | jq -r ".[] | select(.name==\"$name\") | .status" 2>/dev/null)
        if [ "$status" = "Connected" ]; then
            CONNECTED_PLCS+=("$name")
            pass "PLC '$name' connected"
        else
            info "PLC '$name' status: $status (skipping)"
        fi
    done

    if [ ${#CONNECTED_PLCS[@]} -eq 0 ]; then
        fail "No connected PLCs found -- cannot run round-trip tests"
        exit 1
    fi
}

# ── Find a writable DINT on a PLC ────────────────────────────────────────
# Sets globals: FOUND_TAG, FOUND_TAG_ORIGINAL_VALUE
# Returns 0 on success, 1 if no writable DINT found after MAX_DINT_ATTEMPTS.
find_writable_dint() {
    local plc=$1
    FOUND_TAG=""
    FOUND_TAG_ORIGINAL_VALUE=""

    # Get all tags for this PLC (from main tags endpoint + program tags)
    local tags_json all_dints=""
    tags_json=$(api_get "/${plc}/tags")

    if [ -n "$tags_json" ] && [ "$tags_json" != "{}" ]; then
        all_dints=$(echo "$tags_json" | jq -r '
            to_entries[]
            | select(.value.type == "DINT" or .value.type == "INT" or .value.type == "DWORD")
            | .value.name' 2>/dev/null)
    fi

    # Also check program tags for logix PLCs
    local programs
    programs=$(api_get "/${plc}/programs" 2>/dev/null)
    if [ -n "$programs" ] && [ "$programs" != "null" ] && [ "$programs" != "[]" ]; then
        for prog in $(echo "$programs" | jq -r '.[]' 2>/dev/null); do
            local prog_tags
            prog_tags=$(api_get "/${plc}/programs/${prog}/tags" 2>/dev/null)
            if [ -n "$prog_tags" ] && [ "$prog_tags" != "{}" ]; then
                local prog_dints
                prog_dints=$(echo "$prog_tags" | jq -r '
                    to_entries[]
                    | select(.value.type == "DINT" or .value.type == "INT" or .value.type == "DWORD")
                    | .value.name' 2>/dev/null)
                if [ -n "$prog_dints" ]; then
                    all_dints=$(printf "%s\n%s" "$all_dints" "$prog_dints")
                fi
            fi
        done
    fi

    # Deduplicate
    local dint_keys
    dint_keys=$(echo "$all_dints" | sort -u | grep -v '^$')

    if [ -z "$dint_keys" ]; then
        info "  No DINT/INT/DWORD tags on ${plc}"
        return 1
    fi

    local attempt=0
    for tag in $dint_keys; do
        if [ $attempt -ge $MAX_DINT_ATTEMPTS ]; then
            break
        fi
        ((attempt++))

        # Read current value
        local cur
        cur=$(rest_read "$plc" "$tag")
        if [ -z "$cur" ] || [ "$cur" = "null" ]; then
            info "  [${attempt}/${MAX_DINT_ATTEMPTS}] ${plc}/${tag} -- cannot read, skipping"
            continue
        fi

        # Try to write a slightly different value
        local test_val=$(( (RANDOM % 9999) + 1 ))
        # Make sure test value differs from current
        while [ "$test_val" = "$cur" ]; do
            test_val=$(( (RANDOM % 9999) + 1 ))
        done

        local result
        result=$(rest_write "$plc" "$tag" "$test_val")

        if echo "$result" | grep -q '"success":true'; then
            # Wait for PLC poll cycle and re-read
            sleep "$POLL_CYCLE_WAIT"
            local readback
            readback=$(rest_read "$plc" "$tag")

            if [ "$readback" = "$test_val" ]; then
                FOUND_TAG="$tag"
                FOUND_TAG_ORIGINAL_VALUE="$cur"
                info "  [${attempt}/${MAX_DINT_ATTEMPTS}] ${plc}/${tag} is writable (was ${cur}, wrote ${test_val}, read ${readback})"
                return 0
            else
                info "  [${attempt}/${MAX_DINT_ATTEMPTS}] ${plc}/${tag} write accepted but PLC held value (wrote ${test_val}, read ${readback})"
            fi
        else
            local err
            err=$(echo "$result" | jq -r '.error // "unknown"' 2>/dev/null)
            info "  [${attempt}/${MAX_DINT_ATTEMPTS}] ${plc}/${tag} not writable: ${err}"
        fi
    done

    return 1
}

# ── Verify value in each service ──────────────────────────────────────────

verify_rest() {
    local plc=$1 tag=$2 expected=$3
    local val
    val=$(rest_read "$plc" "$tag")
    if [ "$val" = "$expected" ]; then
        pass "REST: ${plc}/${tag} = ${val}"
    else
        fail "REST: ${plc}/${tag} expected ${expected}, got ${val}"
    fi
}

verify_mqtt() {
    local plc=$1 tag=$2 expected=$3
    if [ "$HAVE_MQTT" != true ]; then skip "MQTT: not available"; return; fi

    local topic="${NAMESPACE}/${plc}/tags/${tag}"
    local msg
    msg=$(do_timeout 6 mosquitto_sub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "$topic" -C 1 -W 5 2>/dev/null)
    if [ -z "$msg" ]; then
        fail "MQTT: no message on ${topic}"
        return
    fi

    local val
    val=$(echo "$msg" | jq -r '.value' 2>/dev/null)
    if [ "$val" = "$expected" ]; then
        pass "MQTT: ${topic} value = ${val}"
    else
        fail "MQTT: ${topic} expected ${expected}, got ${val}"
    fi
}

verify_valkey() {
    local plc=$1 tag=$2 expected=$3
    if [ "$HAVE_VALKEY" != true ]; then skip "Valkey: not available"; return; fi

    local key="${NAMESPACE}:${plc}:tags:${tag}"
    local raw
    raw=$(redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" GET "$key" 2>/dev/null)
    if [ -z "$raw" ]; then
        fail "Valkey: key ${key} not found"
        return
    fi

    local val
    val=$(echo "$raw" | jq -r '.value' 2>/dev/null)
    if [ "$val" = "$expected" ]; then
        pass "Valkey: ${key} value = ${val}"
    else
        fail "Valkey: ${key} expected ${expected}, got ${val}"
    fi
}

verify_kafka() {
    local plc=$1 tag=$2 expected=$3
    if [ "$HAVE_KAFKA" != true ]; then skip "Kafka: not available"; return; fi

    # Consume from the namespace topic; look for our plc.tag key.
    # We consume recent messages and grep for the right key.
    local tmpfile="/tmp/wl_kafka_verify_$$"
    do_timeout 6 kcat -b "$KAFKA_BROKER" -t "$NAMESPACE" -C -o -20 -e \
        -f '%k\t%s\n' > "$tmpfile" 2>/dev/null || true

    if [ ! -s "$tmpfile" ]; then
        # Also try consuming from end with a short wait for new messages
        do_timeout 6 kcat -b "$KAFKA_BROKER" -t "$NAMESPACE" -C -o end \
            -f '%k\t%s\n' > "$tmpfile" 2>/dev/null &
        local kpid=$!
        sleep 4
        kill $kpid 2>/dev/null; wait $kpid 2>/dev/null
    fi

    if [ -s "$tmpfile" ]; then
        # Find messages for this plc.tag key (last match wins)
        local line
        line=$(grep "^${plc}\.${tag}	" "$tmpfile" 2>/dev/null | tail -1)
        if [ -z "$line" ]; then
            # Tag might be published with alias - try without key filter
            line=$(grep "\"tag\":\"${tag}\"" "$tmpfile" 2>/dev/null | tail -1)
        fi
        if [ -n "$line" ]; then
            local val
            # Extract the JSON part (after tab)
            val=$(echo "$line" | cut -f2- | jq -r '.value' 2>/dev/null)
            if [ "$val" = "$expected" ]; then
                pass "Kafka: ${plc}.${tag} value = ${val}"
            else
                fail "Kafka: ${plc}.${tag} expected ${expected}, got ${val}"
            fi
        else
            fail "Kafka: no message found for ${plc}.${tag}"
        fi
    else
        fail "Kafka: could not consume from topic ${NAMESPACE}"
    fi
    rm -f "$tmpfile"
}

verify_warcry() {
    local plc=$1 tag=$2 expected=$3
    if [ "$HAVE_WARCRY" != true ]; then skip "Warcry: not available"; return; fi

    # Send a list_tags query and parse the response
    local tmpfile="/tmp/wl_warcry_verify_$$"
    (echo '{"type":"list_tags"}'; sleep 2) | nc -w3 "$WARCRY_HOST" "$WARCRY_PORT" > "$tmpfile" 2>/dev/null || true

    if [ ! -s "$tmpfile" ]; then
        fail "Warcry: no response to list_tags"
        rm -f "$tmpfile"
        return
    fi

    # The response has multiple JSON lines.  Find the tag_list message.
    local tag_list_line
    tag_list_line=$(grep '"tag_list"' "$tmpfile" 2>/dev/null | head -1)
    if [ -z "$tag_list_line" ]; then
        # Maybe it's in a snapshot message
        tag_list_line=$(grep '"snapshot"' "$tmpfile" 2>/dev/null | head -1)
    fi

    if [ -n "$tag_list_line" ]; then
        # Search the tags array for our plc + tag
        local val
        val=$(echo "$tag_list_line" | jq -r "
            .tags[]
            | select(.plc == \"${plc}\" and (.tag == \"${tag}\" or .alias == \"${tag}\"))
            | .value" 2>/dev/null | head -1)
        if [ "$val" = "$expected" ]; then
            pass "Warcry: ${plc}/${tag} value = ${val}"
        elif [ -n "$val" ] && [ "$val" != "null" ]; then
            fail "Warcry: ${plc}/${tag} expected ${expected}, got ${val}"
        else
            fail "Warcry: ${plc}/${tag} not found in tag list"
        fi
    else
        fail "Warcry: could not parse tag list response"
    fi
    rm -f "$tmpfile"
}

# ── Write-back via each service ───────────────────────────────────────────

writeback_rest() {
    local plc=$1 tag=$2 value=$3
    local result
    result=$(rest_write "$plc" "$tag" "$value")
    if echo "$result" | grep -q '"success":true'; then
        return 0
    else
        echo "$result" | jq -r '.error // "unknown error"' 2>/dev/null
        return 1
    fi
}

writeback_mqtt() {
    local plc=$1 tag=$2 value=$3
    if [ "$HAVE_MQTT" != true ]; then return 2; fi

    local topic="${NAMESPACE}/${plc}/write"
    local payload="{\"topic\":\"${NAMESPACE}\",\"plc\":\"${plc}\",\"tag\":\"${tag}\",\"value\":${value}}"

    mosquitto_pub -h "$MQTT_HOST" -p "$MQTT_PORT" -t "$topic" -m "$payload" 2>/dev/null
    return $?
}

writeback_valkey() {
    local plc=$1 tag=$2 value=$3
    if [ "$HAVE_VALKEY" != true ]; then return 2; fi

    local queue="${NAMESPACE}:writes"
    local payload="{\"factory\":\"${NAMESPACE}\",\"plc\":\"${plc}\",\"tag\":\"${tag}\",\"value\":${value}}"

    redis-cli -h "$VALKEY_HOST" -p "$VALKEY_PORT" RPUSH "$queue" "$payload" >/dev/null 2>&1
    return $?
}

writeback_kafka() {
    local plc=$1 tag=$2 value=$3
    if [ "$HAVE_KAFKA" != true ]; then return 2; fi

    local topic="${NAMESPACE}-writes"
    local key="${plc}.${tag}"
    local payload="{\"plc\":\"${plc}\",\"tag\":\"${tag}\",\"value\":${value},\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"

    echo "$payload" | kcat -b "$KAFKA_BROKER" -t "$topic" -P -k "$key" 2>/dev/null
    return $?
}

# ── Run a write-back test for one service ─────────────────────────────────
# Writes $value via the given service, waits, then reads via REST to verify.
test_writeback_service() {
    local service=$1 plc=$2 tag=$3 value=$4
    local rc err

    case "$service" in
        REST)   err=$(writeback_rest   "$plc" "$tag" "$value" 2>&1); rc=$?;;
        MQTT)   err=$(writeback_mqtt   "$plc" "$tag" "$value" 2>&1); rc=$?;;
        Valkey) err=$(writeback_valkey "$plc" "$tag" "$value" 2>&1); rc=$?;;
        Kafka)  err=$(writeback_kafka  "$plc" "$tag" "$value" 2>&1); rc=$?;;
        *)      skip "${service}: unknown service"; return;;
    esac

    if [ $rc -eq 2 ]; then
        skip "${service} writeback: service not available"
        return
    elif [ $rc -ne 0 ]; then
        fail "${service} writeback: send failed ($err)"
        return
    fi

    info "${service} writeback: sent ${value} to ${plc}/${tag}, waiting for PLC..."
    sleep "$PROPAGATION_WAIT"

    local readback
    readback=$(rest_read "$plc" "$tag")
    if [ "$readback" = "$value" ]; then
        pass "${service} writeback: PLC accepted value (REST reads ${readback})"
    else
        fail "${service} writeback: PLC did not accept value (sent ${value}, REST reads ${readback})"
    fi
}

# ── Per-PLC round-trip test ───────────────────────────────────────────────
test_plc_roundtrip() {
    local plc=$1
    section "Round-Trip: ${plc}"

    # Step 1: Find a writable DINT
    info "Searching for a writable DINT/INT/DWORD on ${plc}..."
    if ! find_writable_dint "$plc"; then
        fail "${plc}: could not find a writable integer tag after ${MAX_DINT_ATTEMPTS} attempts"
        return
    fi

    local tag="$FOUND_TAG"
    local orig="$FOUND_TAG_ORIGINAL_VALUE"
    pass "${plc}: using tag '${tag}' (original value: ${orig})"

    # Step 2: Write a test value via REST and verify propagation
    local test_val=$(( (RANDOM % 8000) + 1000 ))
    while [ "$test_val" = "$orig" ]; do
        test_val=$(( (RANDOM % 8000) + 1000 ))
    done

    info "Writing ${test_val} to ${plc}/${tag} via REST..."
    local result
    result=$(rest_write "$plc" "$tag" "$test_val")
    if ! echo "$result" | grep -q '"success":true'; then
        fail "${plc}/${tag}: REST write failed: $(echo "$result" | jq -r '.error' 2>/dev/null)"
        return
    fi
    pass "${plc}/${tag}: REST write accepted"

    info "Waiting ${PROPAGATION_WAIT}s for propagation..."
    sleep "$PROPAGATION_WAIT"

    # Step 3: Verify the value in every service
    echo ""
    log "  --- Publish verification (value = ${test_val}) ---"
    verify_rest    "$plc" "$tag" "$test_val"
    verify_mqtt    "$plc" "$tag" "$test_val"
    verify_valkey  "$plc" "$tag" "$test_val"
    verify_kafka   "$plc" "$tag" "$test_val"
    verify_warcry  "$plc" "$tag" "$test_val"

    # Step 4: Write-back from each service
    echo ""
    log "  --- Write-back tests ---"

    # Each writeback uses a unique value so we can distinguish them
    local wb_rest=$(( (RANDOM % 8000) + 1000 ))
    local wb_mqtt=$(( (RANDOM % 8000) + 1000 ))
    local wb_valkey=$(( (RANDOM % 8000) + 1000 ))
    local wb_kafka=$(( (RANDOM % 8000) + 1000 ))

    # Ensure all are unique and different from test_val
    while [ "$wb_rest" = "$test_val" ]; do wb_rest=$(( (RANDOM % 8000) + 1000 )); done
    while [ "$wb_mqtt" = "$wb_rest" ] || [ "$wb_mqtt" = "$test_val" ]; do wb_mqtt=$(( (RANDOM % 8000) + 1000 )); done
    while [ "$wb_valkey" = "$wb_mqtt" ] || [ "$wb_valkey" = "$wb_rest" ] || [ "$wb_valkey" = "$test_val" ]; do wb_valkey=$(( (RANDOM % 8000) + 1000 )); done
    while [ "$wb_kafka" = "$wb_valkey" ] || [ "$wb_kafka" = "$wb_mqtt" ] || [ "$wb_kafka" = "$wb_rest" ] || [ "$wb_kafka" = "$test_val" ]; do wb_kafka=$(( (RANDOM % 8000) + 1000 )); done

    test_writeback_service "REST"   "$plc" "$tag" "$wb_rest"
    test_writeback_service "MQTT"   "$plc" "$tag" "$wb_mqtt"
    test_writeback_service "Valkey" "$plc" "$tag" "$wb_valkey"
    test_writeback_service "Kafka"  "$plc" "$tag" "$wb_kafka"

    # Warcry is read-only
    skip "Warcry writeback: not supported (read-only streaming protocol)"

    # Step 5: Restore original value
    info "Restoring original value (${orig}) on ${plc}/${tag}..."
    rest_write "$plc" "$tag" "$orig" >/dev/null 2>&1
}

# ── Start warlink (optional) ─────────────────────────────────────────────
maybe_start_warlink() {
    if [ "${1:-}" != "--start" ]; then return; fi

    section "Building & Starting Warlink"

    log "Building warlink..."
    if ! (cd "$SCRIPT_DIR" && go build -o warlink ./cmd/warlink 2>&1 | tee -a "$LOG_FILE"); then
        fail "Build failed"
        exit 1
    fi
    pass "Build successful"

    log "Starting warlink in headless mode..."
    "$SCRIPT_DIR/warlink" -d --log "/tmp/warlink_test_server.log" --log-debug &
    WARLINK_PID=$!

    # Wait for API
    for i in $(seq 1 30); do
        if api_get "/" >/dev/null 2>&1; then
            pass "Warlink started (PID ${WARLINK_PID})"
            # Give PLCs time to connect
            info "Waiting 10s for PLCs to connect..."
            sleep 10
            return
        fi
        sleep 1
    done
    fail "Warlink did not start within 30s"
    kill "$WARLINK_PID" 2>/dev/null
    exit 1
}

# ── Cleanup ───────────────────────────────────────────────────────────────
cleanup() {
    # Kill any background processes we started
    if [ -n "$WARLINK_PID" ]; then
        info "Stopping warlink (PID ${WARLINK_PID})..."
        kill "$WARLINK_PID" 2>/dev/null
        wait "$WARLINK_PID" 2>/dev/null
    fi
    # Clean up temp files
    rm -f /tmp/wl_kafka_verify_$$ /tmp/wl_warcry_verify_$$
}
trap cleanup EXIT

# ── Summary ───────────────────────────────────────────────────────────────
print_summary() {
    section "Test Summary"
    echo ""
    echo -e "  ${GREEN}Passed${NC}:  ${PASS}"
    echo -e "  ${RED}Failed${NC}:  ${FAIL}"
    echo -e "  ${YELLOW}Skipped${NC}: ${SKIP}"
    echo ""
    local total=$((PASS + FAIL))
    if [ $total -gt 0 ]; then
        local pct=$((PASS * 100 / total))
        echo "  Pass rate: ${pct}% (${PASS}/${total})"
    fi
    echo ""
    echo "  Full log: ${LOG_FILE}"
    echo ""

    if [ $FAIL -eq 0 ]; then
        echo -e "  ${GREEN}All tests passed!${NC}"
    else
        echo -e "  ${RED}Some tests failed.${NC}"
    fi
}

# ── Main ──────────────────────────────────────────────────────────────────
main() {
    echo ""
    echo "  WarLink Round-Trip Integration Test"
    echo "  $(date)"
    echo "  ────────────────────────────────────"
    echo "  Namespace: ${NAMESPACE}"
    echo "  API:       ${API_BASE}"
    echo "  MQTT:      ${MQTT_HOST}:${MQTT_PORT}"
    echo "  Valkey:    ${VALKEY_HOST}:${VALKEY_PORT}"
    echo "  Kafka:     ${KAFKA_BROKER}"
    echo "  Warcry:    ${WARCRY_HOST}:${WARCRY_PORT}"
    echo ""

    maybe_start_warlink "$@"
    check_services
    discover_plcs

    for plc in "${CONNECTED_PLCS[@]}"; do
        test_plc_roundtrip "$plc"
    done

    print_summary
    [ $FAIL -eq 0 ]
}

main "$@"
