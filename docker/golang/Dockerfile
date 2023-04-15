FROM golang:1.20-alpine


ENV CFLAGS="-march=native -Ofast -flto"
ENV CXXFLAGS="-march=native -Ofast -flto"
ENV LDFLAGS="-flto"
ENV CGO_CFLAGS="-march=native -Ofast"
ENV GOPROXY="direct"

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

COPY ./ .

# p2pool
RUN go build -buildvcs=false -trimpath -v -o /usr/bin/p2pool git.gammaspectra.live/P2Pool/p2pool-observer/cmd/p2pool

# observer stuff
RUN go build -buildvcs=false -trimpath -v -o /usr/bin/api git.gammaspectra.live/P2Pool/p2pool-observer/cmd/api
RUN go build -buildvcs=false -trimpath -v -o /usr/bin/daemon git.gammaspectra.live/P2Pool/p2pool-observer/cmd/daemon
RUN go build -buildvcs=false -trimpath -v -o /usr/bin/web git.gammaspectra.live/P2Pool/p2pool-observer/cmd/web

WORKDIR /data