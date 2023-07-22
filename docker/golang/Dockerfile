FROM golang:1.21-rc-alpine3.18 AS builder


ENV CFLAGS="-march=native -Ofast"
ENV CXXFLAGS="-march=native -Ofast"
ENV LDFLAGS="-flto"
ENV CGO_CFLAGS="-march=native -Ofast"

RUN apk update && apk add --no-cache \
    git gcc g++ musl-dev bash autoconf automake cmake make libtool gettext

RUN git clone --depth 1 --branch master https://github.com/tevador/RandomX.git /tmp/RandomX && cd /tmp/RandomX && \
    mkdir build && cd build && \
    cmake .. -DCMAKE_BUILD_TYPE=Release -D CMAKE_INSTALL_PREFIX:PATH=/usr && \
    make -j$(nproc) && \
    make install && \
    cd ../ && \
    rm -rf /tmp/RandomX



WORKDIR /usr/src/p2pool

ARG GOPROXY=""
ENV GOPROXY=${GOPROXY}

COPY go.mod go.sum ./
RUN go mod download -x && go mod verify

COPY . .

RUN go run github.com/valyala/quicktemplate/qtc@v1.7.0

ARG BUILD_BINARY

ENV GOEXPERIMENT=loopvar

RUN go build -buildvcs=false -trimpath -v -pgo=auto -o /usr/bin/${BUILD_BINARY} git.gammaspectra.live/P2Pool/p2pool-observer/cmd/${BUILD_BINARY}

FROM alpine:3.18

RUN apk update && apk add --no-cache \
    libstdc++ libgcc

COPY --from=builder /usr/bin/${BUILD_BINARY} /usr/bin/${BUILD_BINARY}

WORKDIR /data