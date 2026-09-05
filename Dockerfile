FROM golang:1.25-alpine
COPY . /app/

WORKDIR /app

RUN go build -o ./bin/app cmd/main.go

CMD ["./bin/app"]