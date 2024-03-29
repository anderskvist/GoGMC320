# Dockerfile References: https://docs.docker.com/engine/reference/builder/

# Start from golang v1.19 base image
FROM golang:1.19

# Add Maintainer Info
LABEL maintainer="Anders Kvist <anderskvist@gmail.com>"

# Set the Current Working Directory inside the container
WORKDIR $GOPATH/src/github.com/anderskvist/GoGMC320

# Copy everything from the current directory to the PWD(Present Working Directory) inside the container
COPY . .

# Download all the dependencies
# https://stackoverflow.com/questions/28031603/what-do-three-dots-mean-in-go-command-line-invocations
RUN go get -d -v ./...

# Install the package
RUN go install -ldflags "-X github.com/anderskvist/GoHelpers/version.Version=`date -u '+%Y%m%d-%H%M%S'`-`git rev-parse --short HEAD`" -v ./...

# Run the executable
CMD ["GoGMC320","/config.ini"]
