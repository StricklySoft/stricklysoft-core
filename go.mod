module github.com/StricklySoft/stricklysoft-core

go 1.22

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.5.5
	github.com/redis/go-redis/v9 v9.5.1
	github.com/minio/minio-go/v7 v7.0.70
	github.com/neo4j/neo4j-go-driver/v5 v5.20.0
	github.com/qdrant/go-client v1.8.0
	go.mongodb.org/mongo-driver v1.15.0
	go.opentelemetry.io/otel v1.26.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.26.0
	go.opentelemetry.io/otel/sdk v1.26.0
	go.opentelemetry.io/otel/trace v1.26.0
	github.com/prometheus/client_golang v1.19.0
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/sony/gobreaker v0.5.0
	google.golang.org/grpc v1.63.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/stretchr/testify v1.9.0
	github.com/testcontainers/testcontainers-go v0.31.0
)
