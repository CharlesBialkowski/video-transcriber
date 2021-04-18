FROM golang:alpine AS Builder


#Set necessary environment variables
ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64

#Move to working directory /build
WORKDIR /build

# Add ca-certificates
RUN apk --no-cache add ca-certificates

#Copy and download dependencies using go mod
COPY go.mod .
COPY go.sum .
RUN go mod tidy
RUN go mod download

#Copy the code into the container
COPY . .

# Build the application
RUN go build -o main .

#Build a small image
FROM scratch

ENV GOOGLE_APPLICATION_CREDENTIALS=/video-transcriber-309519-d926d8f680de.json

COPY --from=Builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=Builder /build/config/video-transcriber-309519-d926d8f680de.json /

COPY --from=Builder /build/main /

EXPOSE 80

# Command to run
ENTRYPOINT ["/main"]
