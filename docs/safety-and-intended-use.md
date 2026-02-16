# Safety and Intended Use

This document describes the intended use of WarLink and important limitations regarding write-back functionality. **Read this document carefully before deploying WarLink in any industrial environment.**

## What WarLink Is Designed For

WarLink is a **data gateway** designed for:

- **Monitoring and dashboards** - Visualizing PLC data in real-time
- **Data logging and historians** - Capturing process data for analysis and compliance
- **Event capture** - Recording snapshots of data at specific trigger points
- **IT integration** - Bridging industrial protocols to modern message brokers (MQTT, Kafka, Redis)
- **Occasional acknowledgments** - Writing simple status flags back to PLCs

## What WarLink Is NOT Designed For

WarLink is **not** a real-time control system and should never be used for:

- Machine control or automation logic
- Safety-critical functions or interlocks
- Process parameter adjustments
- Time-sensitive control loops
- Any function requiring deterministic timing

## Important Technical Limitations

### Read Timing

- PLC reads are **not guaranteed** to occur within a single PLC scan cycle
- Even when tags are batched, individual tag values may be captured at slightly different times
- Network latency, system load, and PLC response times introduce variable delays
- Tag values in a read batch are not atomically consistent

### Write-Back Limitations

- Write-back is implemented as a **best-effort** operation
- Writes may be delayed by network conditions, broker latency, or system load
- There is no guarantee that a write will complete within any specific time window
- Write-backs are not optimized for high-frequency operations
- Kafka writes are deduplicated within batch windows (see [Kafka documentation](kafka.md))

### System Characteristics

- WarLink is a **single-instance application** - not redundant or fault-tolerant
- The application may restart, lose connectivity, or experience delays
- There is no watchdog or heartbeat mechanism to detect application failure
- PLCs should never depend on WarLink for continuous operation

---

## Proper Use of Write-Back

Write-back should only be used for **acknowledgment of events** on **dedicated tags**. The ideal pattern is:

1. The PLC controls all process logic internally
2. The PLC signals WarLink when data is ready to be captured
3. WarLink captures data and publishes to IT systems
4. WarLink writes a simple acknowledgment (success/error) to a dedicated tag
5. The PLC reads the acknowledgment and continues its program

### Dedicated Tags

**Always use tags dedicated to WarLink communication.** These tags should:
- Be written **only** by WarLink (not by PLC logic or other systems)
- Contain simple status values (success/error flags, acknowledgment bits)
- Not be used as inputs to safety logic or process control
- Be reset by the PLC after reading

### Example: Correct Usage - Event Acknowledgment

**Scenario:** A manufacturing process produces serialized parts and requires traceability data to be logged before the part can proceed.

**PLC Program:**
```
// When part is complete, prepare traceability data
IF PartComplete AND NOT DataSaveRequested THEN
    // Populate the data UDT
    TraceData.SerialNumber := CurrentSerial;
    TraceData.Timestamp := CurrentTime;
    TraceData.ProcessValues := CapturedValues;

    // Signal WarLink to capture
    WarLink_SaveData := TRUE;
    DataSaveRequested := TRUE;
END_IF

// Wait for acknowledgment before releasing part
IF DataSaveRequested THEN
    IF WarLink_DataSaved = 1 THEN
        // Success - release part
        ReleasePart();
        ResetWarLinkTags();
    ELSIF WarLink_DataSaved = -1 THEN
        // Error - hold part, alert operator
        HoldPart();
        RaiseAlarm("Traceability save failed");
    END_IF
    // While waiting, part is held - PLC maintains safe state
END_IF
```

**WarLink Configuration:**
```yaml
rules:
  - name: TraceabilityCapture
    conditions:
      - plc: MainPLC
        tag: WarLink_SaveData
        operator: "=="
        value: true
    actions:
      - type: publish
        tag_or_pack: TraceData
        kafka_cluster: all
      - type: writeback
        write_tag: WarLink_DataSaved
        write_value: 1
    cleared_actions:
      - type: writeback
        write_tag: WarLink_DataSaved
        write_value: 0
```

**Why this is correct:**
- PLC maintains full control of the process
- Data is held stable until acknowledgment is received
- Part is safely held if WarLink fails or is delayed
- Write-back is to a dedicated tag, not a process parameter
- PLC program continues to function even if WarLink is unavailable

### Example: Correct Usage - Batch Recipe Selection

**Scenario:** An operator selects a recipe from a dashboard, and the PLC should load it.

**Correct approach:**
```yaml
# Dashboard writes to a dedicated tag
tags:
  - name: WarLink_RecipeRequest
    writable: true    # Dedicated to WarLink

# PLC reads this tag and validates before loading
```

**PLC Program:**
```
// PLC validates and applies recipe - WarLink only suggests
IF WarLink_RecipeRequest > 0 AND NOT RecipeChangeInProgress THEN
    RequestedRecipe := WarLink_RecipeRequest;
    IF ValidateRecipe(RequestedRecipe) THEN
        LoadRecipe(RequestedRecipe);
    ELSE
        RaiseAlarm("Invalid recipe requested");
    END_IF
    WarLink_RecipeRequest := 0;  // Clear the request
END_IF
```

**Why this is correct:**
- PLC validates all inputs before acting
- Recipe is loaded by PLC logic, not directly written
- WarLink only sets a request flag on a dedicated tag
- PLC clears the tag after processing

---

## Incorrect Uses of Write-Back

### Example: UNSAFE - Safety Interlock Control

**Scenario:** An AI camera detects hazardous conditions and should prevent operator entry.

**DANGEROUS - DO NOT DO THIS:**
```yaml
# WRONG: Writing directly to a safety-related tag
tags:
  - name: AreaSafeForEntry
    writable: true    # DANGEROUS!
```

**Why this is dangerous:**
- WarLink is not safety-rated (SIL, PLe, etc.)
- Network delays could allow entry during hazardous conditions
- WarLink could fail, crash, or lose connectivity
- No redundancy or fail-safe behavior
- Violates safety system design principles

**Correct approach:** The AI camera should communicate with a safety-rated system, or the PLC should monitor the camera output directly and implement proper safety logic with appropriate safety-rated hardware.

### Example: WRONG - Direct Parameter Manipulation

**Scenario:** Slow down a drive when outside temperature exceeds 95°F.

**WRONG - DO NOT DO THIS:**
```yaml
# WRONG: Writing directly to a motor speed parameter
tags:
  - name: DriveSpeedSetpoint
    writable: true    # WRONG!
```

**Why this is problematic:**
- Other PLC logic may also be controlling this parameter
- Creates potential for conflicting writes and race conditions
- PLC program cannot reliably know who set the value
- Debugging becomes difficult when values change unexpectedly
- No coordination with other process requirements

**Correct approach:**
```yaml
# RIGHT: Write to a dedicated advisory tag
tags:
  - name: WarLink_HighTempWarning
    writable: true    # Dedicated flag only
```

**PLC Program:**
```
// PLC decides how to respond to advisory
IF WarLink_HighTempWarning AND DriveRunning THEN
    // PLC controls the parameter, considering all factors
    DriveSpeedSetpoint := MIN(DriveSpeedSetpoint, MaxSpeedHighTemp);
END_IF
```

### Example: WRONG - Motion Control Integration

**Scenario:** Send position commands to a servo from an external system.

**WRONG - DO NOT DO THIS:**
```yaml
# WRONG: Writing position commands to motion control
tags:
  - name: ServoTargetPosition
    writable: true    # DANGEROUS!
```

**Why this is dangerous:**
- Motion control requires deterministic, real-time communication
- Network latency could cause erratic motion or collisions
- No coordination with motion controller state machine
- Could command motion when machine is not ready

**Correct approach:** Use dedicated motion networks (EtherCAT, SERCOS, etc.) for position commands. WarLink can write to a dedicated tag that requests a recipe change or mode switch, which the PLC validates and executes.

---

## Summary of Principles

1. **WarLink is for monitoring, not control** - Use it to observe and log, not to make decisions
2. **Dedicated tags only** - Never write to tags that PLC logic also modifies
3. **PLC is the authority** - All process decisions should be made by PLC logic
4. **Acknowledgments, not commands** - Write-back should confirm events, not direct operations
5. **Fail-safe design** - PLC must operate safely if WarLink is unavailable
6. **No safety functions** - Never use WarLink in the safety chain

---

## Disclaimer and Limitation of Liability

**IMPORTANT: READ CAREFULLY BEFORE USING THIS SOFTWARE**

THIS SOFTWARE IS PROVIDED "AS IS" WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE, AND NONINFRINGEMENT.

### No Warranty

The authors and contributors of WarLink make no representations or warranties regarding:
- The suitability of this software for any particular purpose
- The accuracy, reliability, or completeness of any data transmitted
- The timing, latency, or determinism of any operations
- The continuous availability or error-free operation of the software

### Limitation of Liability

IN NO EVENT SHALL THE AUTHORS, CONTRIBUTORS, OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES, OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT, OR OTHERWISE, ARISING FROM, OUT OF, OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

This includes, but is not limited to:
- Personal injury or death
- Property damage
- Production losses
- Data loss or corruption
- Consequential, incidental, special, or punitive damages
- Loss of profits or business interruption

### User Responsibility

By using WarLink, you acknowledge and agree that:

1. **You are solely responsible** for determining whether WarLink is suitable for your application
2. **You are solely responsible** for the safe design and implementation of any system incorporating WarLink
3. **You will not use** WarLink for safety-critical applications, real-time control, or any application where failure could result in injury, death, or significant property damage
4. **You assume all risk** associated with the use of this software
5. **You agree to hold harmless** the authors, contributors, and copyright holders from any claims arising from your use of this software

### Industrial Applications

If you are using WarLink in an industrial environment:

1. Ensure all safety functions are implemented using appropriate safety-rated devices and systems
2. Conduct a thorough risk assessment before deployment
3. Implement appropriate fail-safe mechanisms in your PLC programs
4. Never rely on WarLink for time-critical or safety-critical operations
5. Maintain appropriate backup and redundancy systems independent of WarLink

### Acceptance

**By downloading, installing, or using WarLink, you acknowledge that you have read this disclaimer, understand its terms, and agree to be bound by them.**

If you do not agree to these terms, do not use this software.

---

*This document was last updated to reflect WarLink functionality as of the current release. Users are responsible for reviewing this document with each update.*
