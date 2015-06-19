FROM google/golang:stable

# Godep for vendoring
RUN go get github.com/tools/godep

ENV APP_DIR $GOPATH/service_listener

# Set the entrypoint
ENTRYPOINT ["/opt/app/service_listener"]
ADD . $APP_DIR

# Compile the binary and statically link
RUN mkdir /opt/app
RUN cd $APP_DIR && godep restore
RUN cd $APP_DIR && CGO_ENABLED=0 go build -o /opt/app/service_listener -ldflags '-d -w -s'
