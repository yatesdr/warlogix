# Troubleshooting Guide

This guide covers common issues and solutions organized by component.

## Connection Issues

### All PLCs

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection timeout" | Network unreachable | Verify IP with `ping`, check firewall |
| "Connection refused" | Wrong port or service disabled | Verify port, check PLC settings |
| Intermittent disconnects | Network instability | Check cables, switch ports, use `--log-debug` |
| Slow response | Network congestion or PLC overloaded | Reduce poll rate, check other clients |

### Allen-Bradley (Logix/Micro800)

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "No tags discovered" | PLC in Program mode | Switch to Run mode |
| "Connection rejected" | Too many CIP connections | Close other connections (RSLogix, etc.) |
| Wrong slot error | Incorrect slot number | CompactLogix: slot 0; ControlLogix: check chassis |
| "Path segment error" | Invalid tag path | Verify tag exists, check program scope |

**Debug command:**
```bash
./warlink --log-debug=logix
```

### Siemens S7

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection refused" | PUT/GET not enabled | Enable in TIA Portal (see [PLC Setup](plc-setup.md)) |
| "Access denied" | Optimized block access | Disable on data blocks in TIA Portal |
| "ISO Invalid Buffer" | Wrong rack/slot | S7-1200/1500: use slot 0; S7-300: use slot 2 |
| Wrong values | Byte offset mismatch | Verify offsets match TIA Portal DB layout |
| "PDU negotiation failed" | Firmware incompatibility | Update PLC firmware or try different PDU size |

**Debug command:**
```bash
./warlink --log-debug=s7
```

### Beckhoff TwinCAT

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection reset" | No route configured | Add route in TwinCAT XAE (see [PLC Setup](plc-setup.md)) |
| "Port not found" | Wrong AMS port | TC3: 851, TC2: 801 |
| "No symbols" | PLC not in Run | Activate project and start PLC |
| "ADS error 1808" | Symbol not found | Verify symbol name matches exactly |
| "ADS error 1809" | Symbol access denied | Check TwinCAT security settings |

**Debug command:**
```bash
./warlink --log-debug=ads
```

### Omron FINS

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Timeout" | Wrong port or blocked | Check port 9600 (TCP and UDP) |
| "Wrong node" | FINS node mismatch | Verify node matches PLC rotary switch |
| "Parameter error (0x1103)" | Invalid address | Check memory area and address range |
| TCP refused, UDP works | PLC only supports UDP | Use `protocol: fins-udp` |

**Debug command:**
```bash
./warlink --log-debug=omron
```

### Omron EIP (NJ/NX)

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Tag not found" | Case mismatch | Tag names are case-sensitive |
| "Discovery empty" | Tags not published | Publish tags in Sysmac Studio |
| "Forward Open rejected" | Connection limit | Reduce connection count or use unconnected mode |

**Debug command:**
```bash
./warlink --log-debug=omron
```

---

## Publishing Issues

### MQTT

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection lost" | Broker unreachable | Check broker address and port |
| "Not authorized" | Wrong credentials | Verify username/password |
| No messages appearing | Wrong topic or not subscribed | Use `mosquitto_sub -t "{namespace}/#" -v` |
| Messages delayed | QoS 2 handshake slow | Check broker performance |
| Duplicate client ID | Another client using same ID | Use unique client_id per instance |

**Test connectivity:**
```bash
mosquitto_sub -h localhost -t "warlink1/#" -v
```

### Valkey/Redis

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection refused" | Wrong address/port | Verify address includes port (host:6379) |
| "NOAUTH" | Authentication required | Add password to config |
| Keys not appearing | Wrong namespace | Check `redis-cli KEYS "{namespace}:*"` |
| Pub/Sub not working | publish_changes disabled | Enable in config |
| Write-back not working | enable_writeback disabled | Enable in config |

**Test connectivity:**
```bash
redis-cli -h localhost KEYS "warlink1:*" | head -20
```

### Kafka

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| "Connection refused" | Wrong broker address | Verify bootstrap servers |
| "SASL authentication failed" | Wrong credentials or mechanism | Check username/password and SASL type |
| "Topic not found" | auto_create_topics disabled | Enable or pre-create topics |
| Messages not appearing | Consumer in wrong group | Check consumer group offset |
| High latency | Acks set to -1 (all) | Use acks=1 for lower latency |

**Test connectivity:**
```bash
kcat -b localhost:9092 -L  # List topics
kcat -b localhost:9092 -t warlink1 -C -o end  # Consume new messages
```

---

## TagPack Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| Pack not publishing | All members have ignore_changes=true | At least one member must trigger |
| Missing tags in pack | Source tag not enabled | Enable tag in Browser tab |
| Stale values | PLC disconnected | Check PLC connection status |
| Too many publishes | Volatile tags triggering | Mark volatile tags with ignore_changes |
| Pack publishes but empty | Members from disconnected PLC | Check `plcs` field in output for errors |

---

## Trigger Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| Trigger not firing | Condition not met | Verify trigger tag value meets condition |
| Trigger stuck in "Cooldown" | Condition still true | Condition must go false before re-arming |
| Trigger stuck in "Error" | Previous fire failed | Check logs, press Reset or fix underlying issue |
| Wrong data captured | Tags read at wrong time | Triggers read directly from PLC at fire time |
| Ack tag not written | Tag not marked writable | Enable writable flag on ack tag |
| Test fire works, normal doesn't | Condition edge detection | Trigger fires on rising edge only (false→true) |

**Debug triggers:**
```bash
./warlink --log-debug=kafka,mqtt
```

---

## Performance Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| High CPU usage | Too many tags or fast poll rate | Reduce enabled tags, increase poll_rate |
| Memory growing | Debug logging enabled | Disable `--log-debug` in production |
| Slow UI response | Many tags or complex UDTs | Use filtering, reduce visible tags |
| Broker backpressure | Publishing faster than broker handles | Increase broker resources or reduce publish rate |
| PLC read timeouts | Network latency or PLC overloaded | Increase timeout, reduce poll rate |

**Monitor performance:**
```bash
# Check message rates
mosquitto_sub -h localhost -t "warlink1/#" -v | pv -l > /dev/null

# Check Kafka lag
kcat -b localhost:9092 -t warlink1 -C -o end -q | pv -l > /dev/null
```

---

## Daemon Mode Issues

| Symptom | Likely Cause | Solution |
|---------|--------------|----------|
| SSH connection refused | Wrong port or daemon not running | Check port (--ssh-port flag) and process |
| "Permission denied" | Wrong password or key | Verify --ssh-pass or --ssh-keys |
| Terminal garbled | Client terminal size mismatch | Resize terminal or reconnect |
| Config changes lost | External edit while daemon running | Stop daemon before editing config |
| Daemon exits on disconnect | Not running as daemon | Use `-d` flag, not foreground mode |

---

## Debug Logging

Enable detailed protocol logging to diagnose issues:

```bash
# All protocols (very verbose)
./warlink --log-debug

# Specific protocols
./warlink --log-debug=logix      # Allen-Bradley
./warlink --log-debug=s7         # Siemens
./warlink --log-debug=ads        # Beckhoff
./warlink --log-debug=omron      # Omron (FINS and EIP)
./warlink --log-debug=mqtt       # MQTT publishing
./warlink --log-debug=kafka      # Kafka publishing
./warlink --log-debug=valkey     # Valkey/Redis

# Multiple protocols
./warlink --log-debug=s7,mqtt,kafka

# Write to file
./warlink --log /var/log/warlink.log --log-debug=s7
```

**Warning:** Debug logging generates extremely verbose output. Use only for troubleshooting, not in production.

---

## Getting Help

If you can't resolve an issue:

1. **Check the logs** - Enable debug logging for the relevant component
2. **Verify network** - Use ping, telnet, or protocol-specific tools
3. **Simplify** - Test with a single PLC and single broker
4. **Search issues** - Check [GitHub Issues](https://github.com/yatesdr/warlink/issues)
5. **Open an issue** - Include:
   - WarLink version (`warlink --version`)
   - PLC family and model
   - Relevant config (sanitize passwords)
   - Debug log output
   - Steps to reproduce
