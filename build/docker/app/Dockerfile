FROM golang:1.12 as app

WORKDIR /app
COPY / ./
RUN make build

FROM alpine

ARG CREATED
ARG SOURCE
ARG REVISION

LABEL org.opencontainers.image.created="${CREATED}" \
      org.opencontainers.image.source="${SOURCE}" \
      org.opencontainers.image.revision="${REVISION}" \
      org.opencontainers.image.vendor="OpusCapita" \
      org.opencontainers.image.authors="egor.stambakio@opuscapita.com"

# package 'ca-certificates' is required for https:// requests to not fail with error
# "x509: certificate signed by unknown authority"
RUN apk update && apk add ca-certificates && rm -rf /var/cache/apk/*

WORKDIR /app
COPY --from=app /app/dist/app ./

ENTRYPOINT ["./app"]
