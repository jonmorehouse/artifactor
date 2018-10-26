FROM golang:latest

RUN go get -u cloud.google.com/go/storage

ADD . /src
RUN mkdir -p /go/src/github.com/jonmorehouse && \
	ln -s /src /go/src/github.com/jonmorehouse/artifactor && \
	mkdir /output && \
	cd /src/bin && \
	CGO_ENABLED=0 GOOS=linux go build -o /output/artifactor .

FROM alpine:latest
COPY --from=0 /output/artifactor /bin
ENTRYPOINT ["/bin/artifactor"]
