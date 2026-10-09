#!/bin/sh
# Cria os tópicos do projeto. Roda no container kafka-init a cada subida do ambiente.
set -eu

create_topic() {
  /opt/kafka/bin/kafka-topics.sh \
    --bootstrap-server kafka:9092 \
    --create --if-not-exists \
    --topic "$1" \
    --partitions 1 \
    --replication-factor 1
}

create_topic link.clicked
