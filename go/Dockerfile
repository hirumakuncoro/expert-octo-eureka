FROM golang:1.25-alpine AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/go-service .

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/go-service /app/go-service

ENV PORT=8080
EXPOSE 8080

CMD ["/app/go-service"]
