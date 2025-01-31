FROM golang:1.21.11-alpine AS build-stage
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

ADD . .

RUN go build -o ./Altbot

FROM alpine AS final-stage

COPY --from=build-stage /src/Altbot /usr/local/bin
WORKDIR /data

CMD ["Altbot"]
