FROM alpine

ENTRYPOINT ["/opt/app/route53-kubernetes"]
RUN mkdir -p /opt/app
WORKDIR /opt/app
RUN apk --update add ca-certificates

ADD route53-kubernetes /opt/app/route53-kubernetes
