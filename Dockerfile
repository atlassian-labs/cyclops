FROM --platform=$BUILDPLATFORM golang:1.23 as builder
ARG TARGETPLATFORM
ARG ENVVAR=CGO_ENABLED=0
WORKDIR /go/src/github.com/atlassian-labs/cyclops
COPY go.mod go.sum Makefile ./
COPY cmd cmd
COPY pkg pkg
RUN make build-linux ENVVAR=$ENVVAR

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=builder /go/src/github.com/atlassian-labs/cyclops/bin/linux/cyclops /bin/cyclops
COPY --from=builder /go/src/github.com/atlassian-labs/cyclops/bin/linux/kubectl-cycle /bin/kubectl-cycle
COPY --from=builder /go/src/github.com/atlassian-labs/cyclops/bin/linux/observer /bin/observer
CMD [ "/bin/cyclops" ]
