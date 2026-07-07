FROM golang:1.23 AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /spam-ban-bot .

# distroless/static: CA証明書・tzdata入り、シェルなし、nonrootユーザー
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /spam-ban-bot /spam-ban-bot
ENTRYPOINT ["/spam-ban-bot"]
