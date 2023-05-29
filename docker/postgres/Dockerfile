FROM postgres:15.3


RUN apt-get update \
    && apt-get install -y --no-install-recommends git ca-certificates make gcc postgresql-server-dev-$PG_MAJOR=$PG_VERSION \
    && rm -rf /var/lib/apt/lists/*

RUN git clone --branch v1.5.1 --depth 1 https://github.com/sraoss/pg_ivm.git /usr/src/pg_ivm \
    && cd /usr/src/pg_ivm \
    && make install \
    && cd / \
    && rm -rf /usr/src/pg_ivm
