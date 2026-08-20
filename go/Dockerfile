FROM golang:1.23-alpine AS build

WORKDIR /src

COPY go.mod ./
COPY main.go ./

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/go-service .

FROM alpine:3.20

WORKDIR /app
COPY --from=build /out/go-service /app/go-service

ENV PORT=8080
EXPOSE 8080

CMD ["/app/go-service"]
