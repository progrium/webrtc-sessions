FROM golang:1.21-alpine as builder
WORKDIR /app

COPY go.mod go.sum ./
RUN apk add --no-cache ca-certificates git
RUN go mod download

COPY . ./
RUN CGO_ENABLED=0 go build -v -o sfu-server ./cmd/sfu-server

FROM scratch

COPY --from=builder /app/sfu-server /app/sfu-server
ENTRYPOINT [ "/app/sfu-server" ]
