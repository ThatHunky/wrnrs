FROM golang:1.24-bookworm AS build

WORKDIR /src
RUN apt-get update && apt-get install -y --no-install-recommends gcc ca-certificates && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN GOTOOLCHAIN=local go mod download

COPY . .
RUN GOTOOLCHAIN=local go test ./...
RUN GOTOOLCHAIN=local go build -o /out/wrnrs ./cmd/wrnrs

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates fonts-dejavu-core && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=build /out/wrnrs /usr/local/bin/wrnrs
COPY content ./content
COPY assets ./assets
COPY migrations ./migrations

EXPOSE 8080
CMD ["wrnrs"]
