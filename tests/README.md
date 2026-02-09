# Integration Tests

This directory contains integration tests for WarLink.

## Important Note

The `integration_test.sh` script is designed for our internal test environment with specific PLC hardware and broker configurations. It is provided as a **reference implementation** for building your own integration tests tailored to your environment.

## Adapting for Your Environment

To use these tests in your own setup, you'll need to modify:

- **Namespace**: Update `NAMESPACE` to match your config
- **PLC names**: Replace `logix_L7`, `micro820`, `s7`, `beckhoff1`, etc. with your PLC identifiers
- **Tag names**: Update tag references to match your PLC programs
- **Broker endpoints**: Adjust MQTT, Kafka, and Valkey connection details as needed

## Running

```bash
./tests/integration_test.sh
```

Requires: `curl`, `jq`, `mosquitto_sub`, `redis-cli`, `kcat` (or `kafkacat`)
