FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o hookwatch .

FROM alpine:3.19
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=builder /app/hookwatch .
EXPOSE 8877
ENTRYPOINT ["./hookwatch"]
CMD ["--port", "8877"]
