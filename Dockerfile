FROM golang:1.13-alpine3.10 AS builder
WORKDIR /go/src/app
COPY go.mod go.sum *.go ./
RUN go build ./...
RUN ls -l /go/bin /go/src/app

FROM alpine:3.10
ENV PORT=8082
ENV ONYXCHAIN_NET=main
RUN apk add --no-cache gettext && \
    addgroup app && \
    adduser -G app -s /bin/sh -D app -h /home/app
USER app
WORKDIR /home/app
COPY --from=builder /go/src/app/onyxchain-holder ./
COPY log4go.xml install.sql config.json.template run.sh ./
EXPOSE ${PORT}
CMD ["sh", "run.sh"]