FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /mock-server .

FROM nicolaka/netshoot:v0.14
WORKDIR /
COPY --from=builder /mock-server /mock-server
EXPOSE 8080
ENTRYPOINT ["/mock-server"]
