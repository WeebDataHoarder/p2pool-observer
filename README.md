

## Operational instructions

## Update / Apply new settings
```bash
$ git pull && docker-compose build --pull && docker-compose up -d
```

## Backfill likely sweep transactions
```bash
$ docker-compose exec --workdir /usr/src/p2pool -e TRANSACTION_LOOKUP_OTHER=https://p2pool.observer daemon \
go run -v git.gammaspectra.live/P2Pool/p2pool-observer/cmd/scansweeps \
-host MONEROD_HOST -rpc-port MONEROD_RPC_PORT \
-api-host "http://p2pool:3131" \
-db="host=db port=5432 dbname=p2pool user=p2pool password=p2pool sslmode=disable"
```

Can also specify `-e TRANSACTION_LOOKUP_OTHER=https://OTHER_INSTANCE` to query other instance additionally