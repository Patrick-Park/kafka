package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/optiopay/kafka"
	"github.com/optiopay/kafka/proto"
)

func TestProducerBrokenConnection(t *testing.T) {
	topics := []string{"Topic3", "Topic4"}

	cluster := NewLocalCluster(4, "/home/satran/src/kafka/kafka_2.11-0.10.0.0")
	defer func() {
		err := cluster.Stop()
		if err != nil {
			t.Error(err)
		}
	}()
	if err := cluster.Start(); err != nil {
		t.Fatal(err)
	}
	bconf := kafka.NewBrokerConf("producer-broken-connection")
	bconf.AllowTopicCreation = true
	bconf.Logger = &testLogger{t}
	addrs := cluster.KafkaAddrs()
	broker, err := kafka.Dial(addrs, bconf)
	if err != nil {
		t.Fatalf("cannot connect to cluster (%q): %s", addrs, err)
	}
	defer broker.Close()

	// produce big message to enforce TCP buffer flush
	m := proto.Message{
		Value: []byte(strings.Repeat("producer broken connection message ", 1000)),
	}
	pconf := kafka.NewProducerConf()
	producer := broker.Producer(pconf)

	println("sending messages to brokers")
	// send message to all topics to make sure it's working
	for _, name := range topics {
		if _, err := producer.Produce(name, 0, &m); err != nil {
			t.Fatalf("cannot produce to %q: %s", name, err)
		}
	}

	// close two kafka clusters and publish to all 3 topics - 2 of them should
	// retry sending, because lack of leader makes the request fail
	//
	// request should not succeed until nodes are back - bring them back after
	// small delay and make sure producing was successful
	var stopped []*kafkaProcess
	println("stopping container")
	for _, k := range cluster.Kafkas {
		if err := k.Stop(); err != nil {
			println(err.Error())
			t.Fatalf("cannot kill %q kafka container: %s", k.brokerID, err)
		}
		stopped = append(stopped, k)
		if len(stopped) == 2 {
			break
		}
	}

	println("starting again")
	// bring stopped containers back
	errc := make(chan error)
	go func() {
		time.Sleep(10 * time.Second)
		for _, container := range stopped {
			if err := container.Start(); err != nil {
				errc <- err
			}
		}
		close(errc)
	}()

	println("producing messages")
	// send message to all topics to make sure it's working
	for _, name := range topics {
		if _, err := producer.Produce(name, 0, &m); err != nil {
			t.Errorf("cannot produce to %q: %s", name, err)
		}
	}

	println("waiting for errors")
	for err := range errc {
		t.Errorf("cannot start container: %s", err)
	}

	println("waiting for consuming")
	// make sure data was persisted
	for _, name := range topics {
		println("consuming", name)
		consumer, err := broker.Consumer(kafka.NewConsumerConf(name, 0))
		if err != nil {
			t.Errorf("cannot create consumer for %q: %s", name, err)
			continue
		}
		for i := 0; i < 2; i++ {
			if _, err := consumer.Consume(); err != nil {
				t.Errorf("cannot consume %d message from %q: %s", i, name, err)
			}
			println("consumend", name, i)
		}
	}
	println("============== done ==========")
}

func TestConsumerBrokenConnection(t *testing.T) {
	IntegrationTest(t)
	const msgPerTopic = 10

	topics := []string{"Topic3", "Topic4"}

	cluster := NewKafkaCluster("kafka-docker/", 4)
	if err := cluster.Start(); err != nil {
		t.Fatalf("cannot start kafka cluster: %s", err)
	}
	defer func() {
		_ = cluster.Stop()
	}()

	bconf := kafka.NewBrokerConf("producer-broken-connection")
	bconf.Logger = &testLogger{t}
	addrs, err := cluster.KafkaAddrs()
	if err != nil {
		t.Fatalf("cannot get kafka address: %s", err)
	}
	broker, err := kafka.Dial(addrs, bconf)
	if err != nil {
		t.Fatalf("cannot connect to cluster (%q): %s", addrs, err)
	}
	defer broker.Close()

	// produce big message to enforce TCP buffer flush
	m := proto.Message{
		Value: []byte(strings.Repeat("consumer broken connection message ", 1000)),
	}
	pconf := kafka.NewProducerConf()
	producer := broker.Producer(pconf)

	// send message to all topics
	for _, name := range topics {
		for i := 0; i < msgPerTopic; i++ {
			if _, err := producer.Produce(name, 0, &m); err != nil {
				t.Fatalf("cannot produce to %q: %s", name, err)
			}
		}
	}

	// close two kafka clusters and publish to all 3 topics - 2 of them should
	// retry sending, because lack of leader makes the request fail
	//
	// request should not succeed until nodes are back - bring them back after
	// small delay and make sure producing was successful
	containers, err := cluster.Containers()
	if err != nil {
		t.Fatalf("cannot get containers: %s", err)
	}
	var stopped []*Container
	for _, container := range containers {
		if container.RunningKafka() {
			if err := container.Kill(); err != nil {
				t.Fatalf("cannot kill %q kafka container: %s", container.ID, err)
			}
			stopped = append(stopped, container)
		}
		if len(stopped) == 2 {
			break
		}
	}

	// bring stopped containers back
	errc := make(chan error)
	go func() {
		time.Sleep(500 * time.Millisecond)
		for _, container := range stopped {
			if err := container.Start(); err != nil {
				errc <- err
			}
		}
		close(errc)
	}()

	// make sure data was persisted
	for _, name := range topics {
		consumer, err := broker.Consumer(kafka.NewConsumerConf(name, 0))
		if err != nil {
			t.Errorf("cannot create consumer for %q: %s", name, err)
			continue
		}
		for i := 0; i < msgPerTopic; i++ {
			if _, err := consumer.Consume(); err != nil {
				t.Errorf("cannot consume %d message from %q: %s", i, name, err)
			}
		}
	}

	for err := range errc {
		t.Errorf("cannot start container: %s", err)
	}
}

func TestNewTopic(t *testing.T) {
	IntegrationTest(t)
	const msgPerTopic = 10

	topic := "NewTopic"

	cluster := NewKafkaCluster("kafka-docker/", 1)
	if err := cluster.Start(); err != nil {
		t.Fatalf("cannot start kafka cluster: %s", err)
	}
	defer func() {
		_ = cluster.Stop()
	}()

	bconf := kafka.NewBrokerConf("producer-new-topic")
	bconf.Logger = &testLogger{t}
	bconf.AllowTopicCreation = true
	addrs, err := cluster.KafkaAddrs()
	if err != nil {
		t.Fatalf("cannot get kafka address: %s", err)
	}
	broker, err := kafka.Dial(addrs, bconf)
	if err != nil {
		t.Fatalf("cannot connect to cluster (%q): %s", addrs, err)
	}
	defer broker.Close()

	m := proto.Message{
		Value: []byte("Test message"),
	}
	pconf := kafka.NewProducerConf()
	producer := broker.Producer(pconf)

	if _, err := producer.Produce(topic, 0, &m); err != nil {
		t.Fatalf("cannot produce to %q: %s", topic, err)
	}

	consumer, err := broker.Consumer(kafka.NewConsumerConf(topic, 0))
	if err != nil {
		t.Fatalf("cannot create consumer for %q: %s", topic, err)
	}
	if _, err := consumer.Consume(); err != nil {
		t.Errorf("cannot consume message from %q: %s", topic, err)
	}
}

type testLogger struct {
	*testing.T
}

func (tlog *testLogger) Debug(msg string, keyvals ...interface{}) {
	args := append([]interface{}{msg}, keyvals...)
	tlog.Log(args...)
}

func (tlog *testLogger) Info(msg string, keyvals ...interface{}) {
	args := append([]interface{}{msg}, keyvals...)
	tlog.Log(args...)
}

func (tlog *testLogger) Warn(msg string, keyvals ...interface{}) {
	args := append([]interface{}{msg}, keyvals...)
	tlog.Log(args...)
}

func (tlog *testLogger) Error(msg string, keyvals ...interface{}) {
	args := append([]interface{}{msg}, keyvals...)
	tlog.Log(args...)
}
