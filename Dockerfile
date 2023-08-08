FROM registry.ci.openshift.org/openshift/release:golang-1.19 AS builder
WORKDIR /app/
COPY . .
RUN go build -mod vendor -o /tmp/kourier ./cmd/kourier

FROM openshift/origin-base

USER 65532

COPY --from=builder /tmp/kourier /ko-app/kourier
ENTRYPOINT ["/ko-app/kourier"]
