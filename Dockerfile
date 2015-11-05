FROM golang:1.5

# Godep for vendoring
RUN go get github.com/tools/godep

ENV APP_DIR $GOPATH/route53-kubernetes

# Set the entrypoint
ENTRYPOINT ["/opt/app/route53-kubernetes"]
ADD . $APP_DIR

# Compile the binary and statically link
RUN mkdir /opt/app
RUN cd $APP_DIR && godep restore
RUN cd $APP_DIR && go build -o /opt/app/route53-kubernetes
