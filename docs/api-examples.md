# API Consumer Examples

This guide provides code examples for consuming WarLink data in various languages.

## Python

### MQTT Subscriber

```python
import json
import paho.mqtt.client as mqtt

def on_connect(client, userdata, flags, rc):
    print(f"Connected with result code {rc}")
    # Subscribe to all tags from all PLCs
    client.subscribe("factory/#")

def on_message(client, userdata, msg):
    try:
        payload = json.loads(msg.payload.decode())

        # Tag value message
        if "/tags/" in msg.topic:
            print(f"Tag: {payload['plc']}/{payload['tag']} = {payload['value']} ({payload['type']})")

        # Health message
        elif "/health" in msg.topic:
            status = "ONLINE" if payload['online'] else "OFFLINE"
            print(f"Health: {payload['plc']} is {status}")

        # TagPack message
        elif "/packs/" in msg.topic:
            print(f"Pack: {payload['name']} with {len(payload['tags'])} tags")
            for key, data in payload['tags'].items():
                print(f"  {key} = {data['value']}")

        # Trigger message
        elif "/triggers/" in msg.topic:
            print(f"Trigger: {payload['trigger']} fired (seq: {payload['sequence']})")
            for tag, value in payload['data'].items():
                print(f"  {tag} = {value}")

    except json.JSONDecodeError:
        print(f"Invalid JSON: {msg.payload}")

client = mqtt.Client()
client.on_connect = on_connect
client.on_message = on_message

client.connect("localhost", 1883, 60)
client.loop_forever()
```

### MQTT Write Request

```python
import json
import paho.mqtt.client as mqtt

def write_tag(plc, tag, value, namespace="factory"):
    client = mqtt.Client()
    client.connect("localhost", 1883)

    payload = {
        "topic": namespace,
        "plc": plc,
        "tag": tag,
        "value": value
    }

    topic = f"{namespace}/{plc}/write"
    client.publish(topic, json.dumps(payload))
    client.disconnect()

# Example: Write a setpoint
write_tag("Line1_PLC", "Program:Main.Setpoint", 100.5)
```

### Kafka Consumer

```python
from kafka import KafkaConsumer
import json

consumer = KafkaConsumer(
    'factory',
    'factory.health',
    bootstrap_servers=['localhost:9092'],
    value_deserializer=lambda m: json.loads(m.decode('utf-8')),
    auto_offset_reset='latest',
    group_id='my-consumer-group'
)

for message in consumer:
    data = message.value

    # Check message type by fields present
    if 'trigger' in data:
        print(f"Trigger: {data['trigger']} seq={data['sequence']}")
    elif 'online' in data:
        print(f"Health: {data['plc']} online={data['online']}")
    elif 'name' in data and 'tags' in data:
        print(f"Pack: {data['name']}")
    else:
        print(f"Tag: {data['plc']}/{data['tag']} = {data['value']}")
```

### Redis/Valkey Client

```python
import redis
import json

r = redis.Redis(host='localhost', port=6379, decode_responses=True)

# Read a specific tag
key = "factory:Line1_PLC:tags:Counter"
value = r.get(key)
if value:
    data = json.loads(value)
    print(f"Counter = {data['value']}")

# Read all tags for a PLC
keys = r.keys("factory:Line1_PLC:tags:*")
for key in keys:
    data = json.loads(r.get(key))
    print(f"{data['tag']} = {data['value']}")

# Subscribe to changes
pubsub = r.pubsub()
pubsub.subscribe("factory:Line1_PLC:changes")

for message in pubsub.listen():
    if message['type'] == 'message':
        data = json.loads(message['data'])
        print(f"Changed: {data['tag']} = {data['value']}")
```

### REST API Client

```python
import requests

BASE_URL = "http://localhost:8080"

# List all PLCs
response = requests.get(f"{BASE_URL}/")
plcs = response.json()
for plc in plcs:
    print(f"{plc['name']}: {plc['status']}")

# Get tags for a PLC
response = requests.get(f"{BASE_URL}/Line1_PLC/tags")
tags = response.json()
for tag in tags:
    print(f"{tag['name']} = {tag['value']}")

# Read a specific tag
response = requests.get(f"{BASE_URL}/Line1_PLC/tags/Counter")
tag = response.json()
print(f"Counter = {tag['value']}")

# Write a tag
response = requests.post(
    f"{BASE_URL}/Line1_PLC/write",
    json={
        "plc": "Line1_PLC",
        "tag": "Setpoint",
        "value": 100
    }
)
result = response.json()
if result['success']:
    print("Write successful")
else:
    print(f"Write failed: {result['error']}")
```

---

## Node.js / JavaScript

### MQTT Subscriber

```javascript
const mqtt = require('mqtt');

const client = mqtt.connect('mqtt://localhost:1883');

client.on('connect', () => {
    console.log('Connected to MQTT broker');
    client.subscribe('factory/#');
});

client.on('message', (topic, message) => {
    try {
        const payload = JSON.parse(message.toString());

        if (topic.includes('/tags/')) {
            console.log(`Tag: ${payload.plc}/${payload.tag} = ${payload.value}`);
        } else if (topic.includes('/health')) {
            console.log(`Health: ${payload.plc} is ${payload.online ? 'ONLINE' : 'OFFLINE'}`);
        } else if (topic.includes('/packs/')) {
            console.log(`Pack: ${payload.name}`);
            Object.entries(payload.tags).forEach(([key, data]) => {
                console.log(`  ${key} = ${data.value}`);
            });
        } else if (topic.includes('/triggers/')) {
            console.log(`Trigger: ${payload.trigger} seq=${payload.sequence}`);
        }
    } catch (e) {
        console.error('Invalid JSON:', e.message);
    }
});
```

### Kafka Consumer (kafkajs)

```javascript
const { Kafka } = require('kafkajs');

const kafka = new Kafka({
    clientId: 'my-app',
    brokers: ['localhost:9092']
});

const consumer = kafka.consumer({ groupId: 'my-group' });

async function run() {
    await consumer.connect();
    await consumer.subscribe({ topics: ['factory', 'factory.health'], fromBeginning: false });

    await consumer.run({
        eachMessage: async ({ topic, partition, message }) => {
            const data = JSON.parse(message.value.toString());

            if (data.trigger) {
                console.log(`Trigger: ${data.trigger}`);
            } else if (data.online !== undefined) {
                console.log(`Health: ${data.plc} online=${data.online}`);
            } else {
                console.log(`Tag: ${data.plc}/${data.tag} = ${data.value}`);
            }
        },
    });
}

run().catch(console.error);
```

### Redis Client (ioredis)

```javascript
const Redis = require('ioredis');

const redis = new Redis();

// Read a tag
async function readTag(plc, tag) {
    const key = `factory:${plc}:tags:${tag}`;
    const value = await redis.get(key);
    return value ? JSON.parse(value) : null;
}

// Subscribe to changes
const subscriber = new Redis();
subscriber.subscribe('factory:Line1_PLC:changes');

subscriber.on('message', (channel, message) => {
    const data = JSON.parse(message);
    console.log(`Changed: ${data.tag} = ${data.value}`);
});

// Example usage
readTag('Line1_PLC', 'Counter').then(data => {
    console.log(`Counter = ${data.value}`);
});
```

### REST API (fetch)

```javascript
const BASE_URL = 'http://localhost:8080';

// List PLCs
async function listPLCs() {
    const response = await fetch(`${BASE_URL}/`);
    return response.json();
}

// Read tag
async function readTag(plc, tag) {
    const response = await fetch(`${BASE_URL}/${plc}/tags/${tag}`);
    return response.json();
}

// Write tag
async function writeTag(plc, tag, value) {
    const response = await fetch(`${BASE_URL}/${plc}/write`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ plc, tag, value })
    });
    return response.json();
}

// Example usage
(async () => {
    const plcs = await listPLCs();
    console.log('PLCs:', plcs.map(p => p.name).join(', '));

    const counter = await readTag('Line1_PLC', 'Counter');
    console.log(`Counter = ${counter.value}`);
})();
```

---

## Go

### MQTT Subscriber

```go
package main

import (
    "encoding/json"
    "fmt"
    mqtt "github.com/eclipse/paho.mqtt.golang"
)

type TagMessage struct {
    PLC       string      `json:"plc"`
    Tag       string      `json:"tag"`
    Value     interface{} `json:"value"`
    Type      string      `json:"type"`
    Timestamp string      `json:"timestamp"`
}

func main() {
    opts := mqtt.NewClientOptions()
    opts.AddBroker("tcp://localhost:1883")
    opts.SetClientID("go-consumer")

    opts.SetDefaultPublishHandler(func(client mqtt.Client, msg mqtt.Message) {
        var data TagMessage
        if err := json.Unmarshal(msg.Payload(), &data); err != nil {
            fmt.Printf("Error: %v\n", err)
            return
        }
        fmt.Printf("Tag: %s/%s = %v\n", data.PLC, data.Tag, data.Value)
    })

    client := mqtt.NewClient(opts)
    if token := client.Connect(); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    if token := client.Subscribe("factory/#", 0, nil); token.Wait() && token.Error() != nil {
        panic(token.Error())
    }

    select {} // Block forever
}
```

### Kafka Consumer

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "github.com/segmentio/kafka-go"
)

func main() {
    reader := kafka.NewReader(kafka.ReaderConfig{
        Brokers: []string{"localhost:9092"},
        Topic:   "factory",
        GroupID: "go-consumer",
    })
    defer reader.Close()

    for {
        msg, err := reader.ReadMessage(context.Background())
        if err != nil {
            fmt.Printf("Error: %v\n", err)
            continue
        }

        var data map[string]interface{}
        json.Unmarshal(msg.Value, &data)

        if plc, ok := data["plc"].(string); ok {
            if tag, ok := data["tag"].(string); ok {
                fmt.Printf("Tag: %s/%s = %v\n", plc, tag, data["value"])
            }
        }
    }
}
```

### REST Client

```go
package main

import (
    "encoding/json"
    "fmt"
    "io"
    "net/http"
)

type TagResponse struct {
    PLC       string      `json:"plc"`
    Name      string      `json:"name"`
    Value     interface{} `json:"value"`
    Type      string      `json:"type"`
    Timestamp string      `json:"timestamp"`
}

func main() {
    resp, err := http.Get("http://localhost:8080/Line1_PLC/tags/Counter")
    if err != nil {
        panic(err)
    }
    defer resp.Body.Close()

    body, _ := io.ReadAll(resp.Body)

    var tag TagResponse
    json.Unmarshal(body, &tag)

    fmt.Printf("%s = %v\n", tag.Name, tag.Value)
}
```

---

## C# / .NET

### MQTT Subscriber (MQTTnet)

```csharp
using MQTTnet;
using MQTTnet.Client;
using System.Text;
using System.Text.Json;

var factory = new MqttFactory();
var client = factory.CreateMqttClient();

var options = new MqttClientOptionsBuilder()
    .WithTcpServer("localhost", 1883)
    .Build();

client.ApplicationMessageReceivedAsync += e =>
{
    var payload = Encoding.UTF8.GetString(e.ApplicationMessage.PayloadSegment);
    var data = JsonSerializer.Deserialize<Dictionary<string, JsonElement>>(payload);

    if (data.TryGetValue("plc", out var plc) && data.TryGetValue("tag", out var tag))
    {
        Console.WriteLine($"Tag: {plc}/{tag} = {data["value"]}");
    }

    return Task.CompletedTask;
};

await client.ConnectAsync(options);
await client.SubscribeAsync("factory/#");

Console.WriteLine("Press any key to exit");
Console.ReadKey();
```

### REST Client (HttpClient)

```csharp
using System.Net.Http.Json;

var client = new HttpClient { BaseAddress = new Uri("http://localhost:8080") };

// Read tags
var tags = await client.GetFromJsonAsync<List<TagResponse>>("/Line1_PLC/tags");
foreach (var tag in tags)
{
    Console.WriteLine($"{tag.Name} = {tag.Value}");
}

// Write a tag
var writeRequest = new { plc = "Line1_PLC", tag = "Setpoint", value = 100 };
var response = await client.PostAsJsonAsync("/Line1_PLC/write", writeRequest);
var result = await response.Content.ReadFromJsonAsync<WriteResponse>();

Console.WriteLine(result.Success ? "Write successful" : $"Failed: {result.Error}");

record TagResponse(string Plc, string Name, object Value, string Type, string Timestamp);
record WriteResponse(bool Success, string Error);
```

---

## Shell / CLI Tools

### MQTT (mosquitto)

```bash
# Subscribe to all messages
mosquitto_sub -h localhost -t "factory/#" -v

# Subscribe to specific PLC
mosquitto_sub -h localhost -t "factory/Line1_PLC/tags/+" -v

# Subscribe to health only
mosquitto_sub -h localhost -t "factory/+/health" -v

# Write a value
mosquitto_pub -h localhost -t "factory/Line1_PLC/write" \
  -m '{"topic":"factory","plc":"Line1_PLC","tag":"Setpoint","value":100}'
```

### Kafka (kcat/kafkacat)

```bash
# Consume messages
kcat -b localhost:9092 -t factory -C

# Consume with key
kcat -b localhost:9092 -t factory -C -K ":"

# Consume specific number
kcat -b localhost:9092 -t factory -C -c 10

# Produce write request
echo '{"plc":"Line1_PLC","tag":"Setpoint","value":100}' | \
  kcat -b localhost:9092 -t factory-writes -P -k "Line1_PLC.Setpoint"
```

### Redis (redis-cli)

```bash
# Get a tag value
redis-cli GET "factory:Line1_PLC:tags:Counter"

# Get all tags for a PLC
redis-cli KEYS "factory:Line1_PLC:tags:*"

# Subscribe to changes
redis-cli SUBSCRIBE "factory:Line1_PLC:changes"

# Write request (push to queue)
redis-cli RPUSH "factory:writes" \
  '{"factory":"factory","plc":"Line1_PLC","tag":"Setpoint","value":100}'
```

### REST (curl)

```bash
# List PLCs
curl http://localhost:8080/

# Get tags
curl http://localhost:8080/Line1_PLC/tags

# Get specific tag
curl http://localhost:8080/Line1_PLC/tags/Counter

# Write a tag
curl -X POST http://localhost:8080/Line1_PLC/write \
  -H "Content-Type: application/json" \
  -d '{"plc":"Line1_PLC","tag":"Setpoint","value":100}'

# Get TagPack
curl http://localhost:8080/tagpack/ProductionMetrics
```

---

## Data Format Reference

### Tag Message

```json
{
  "plc": "Line1_PLC",
  "tag": "Counter",
  "value": 42,
  "type": "DINT",
  "writable": false,
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### Health Message

```json
{
  "plc": "Line1_PLC",
  "online": true,
  "status": "connected",
  "error": "",
  "timestamp": "2024-01-15T10:30:00Z"
}
```

### TagPack Message

```json
{
  "name": "ProductionMetrics",
  "timestamp": "2024-01-15T10:30:00Z",
  "tags": {
    "Line1_PLC.Counter": {
      "value": 42,
      "type": "DINT",
      "plc": "Line1_PLC"
    }
  }
}
```

### Trigger Message

```json
{
  "trigger": "PartComplete",
  "timestamp": "2024-01-15T10:30:00.123456789Z",
  "sequence": 42,
  "plc": "Line1_PLC",
  "metadata": {
    "line": "Line1"
  },
  "data": {
    "SerialNumber": "ABC123",
    "Weight": 1.5
  }
}
```
