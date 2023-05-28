CREATE TABLE IF NOT EXISTS miners (
    id bigint PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    alias varchar UNIQUE DEFAULT NULL,
    spend_public_key bytea NOT NULL,
    view_public_key bytea NOT NULL,
    UNIQUE (spend_public_key, view_public_key)
);


CREATE TABLE IF NOT EXISTS side_blocks (
    main_id bytea PRIMARY KEY, -- mainchain id, on Monero network
    main_height bigint NOT NULL, -- mainchain height

    template_id bytea NOT NULL, -- sidechain template id. Note multiple blocks can exist per template id, see inclusion
    side_height bigint NOT NULL, -- sidechain height
    parent_template_id bytea NOT NULL, -- previous sidechain template id

    miner bigint NOT NULL, -- miner who contributed the block

    -- uncle inclusion information
    uncle_of bytea DEFAULT NULL, -- has been included under this parent block template id as an uncle. Can change after insert
    effective_height bigint NOT NULL, -- has been included under this parent block height as an uncle, or is this height. Can change after insert

    -- nonce data
    nonce bigint NOT NULL, -- nonce on block header. requires bigint for unsigned int32
    extra_nonce bigint NOT NULL, -- nonce on coinbase transaction extra data. requires bigint for unsigned int32

    -- other not indexed data
    timestamp bigint NOT NULL, -- mainchain timestamp
    software_id bigint NOT NULL, -- Software used to generate this template. requires bigint for unsigned int32
    software_version bigint NOT NULL, -- Software version used to generate this template. requires bigint for unsigned int32
    window_depth int NOT NULL, -- PPLNS window depth, in blocks including this one
    window_outputs int NOT NULL, -- number of outputs on coinbase transaction
    transaction_count int NOT NULL, -- number of transactions included in the template

    difficulty bigint NOT NULL, -- sidechain difficulty at height
    cumulative_difficulty bytea NOT NULL, -- sidechain cumulative difficulty at height, binary
    pow_difficulty bigint NOT NULL, -- difficulty of pow_hash
    pow_hash bytea NOT NULL, -- result of PoW function as a hash (all 0x00 = not known)

    inclusion int NOT NULL DEFAULT 1, -- how the block is included. Can change after insert:
    -- 0 = orphan (was not included in-verified-chain)
    -- 1 = in-verified-chain (uncle or main)
    -- 2 = alternate in-verified-chain (uncle or main), for example when duplicate nonce happens
    -- Higher values might specify forks or other custom additions

    UNIQUE (template_id, nonce, extra_nonce), -- main id can only change when nonce / extra nonce is adjusted, as template_id hash does not include them
    FOREIGN KEY (miner) REFERENCES miners (id)
);

CREATE INDEX IF NOT EXISTS side_blocks_miner_idx ON side_blocks (miner);
CREATE INDEX IF NOT EXISTS side_blocks_template_id_idx ON side_blocks (template_id);
CREATE INDEX IF NOT EXISTS side_blocks_main_height_idx ON side_blocks (main_height);
CREATE INDEX IF NOT EXISTS side_blocks_side_height_idx ON side_blocks (side_height);
CREATE INDEX IF NOT EXISTS side_blocks_parent_template_id_idx ON side_blocks (parent_template_id);
CREATE INDEX IF NOT EXISTS side_blocks_uncle_of_idx ON side_blocks (uncle_of);
CREATE INDEX IF NOT EXISTS side_blocks_effective_height_idx ON side_blocks (effective_height);

-- CLUSTER VERBOSE side_blocks USING side_blocks_effective_height_idx;

-- Cannot have non-unique constraints
-- ALTER TABLE side_blocks ADD CONSTRAINT fk_side_blocks_uncle_of FOREIGN KEY (uncle_of) REFERENCES side_blocks (template_id);
-- ALTER TABLE side_blocks ADD CONSTRAINT fk_side_blocks_effective_height FOREIGN KEY (effective_height) REFERENCES side_blocks (side_height);


CREATE TABLE IF NOT EXISTS main_blocks (
    id bytea PRIMARY KEY,
    height bigint UNIQUE NOT NULL,
    timestamp bigint NOT NULL, -- timestamp as set in block
    reward bigint NOT NULL,
    coinbase_id bytea UNIQUE NOT NULL,
    difficulty bigint NOT NULL, -- mainchain difficulty at height
    metadata jsonb NOT NULL DEFAULT '{}', -- metadata such as pool ownership, links to other p2pool networks, and other interesting data
    -- sidechain data for blocks we own
    side_template_id bytea UNIQUE DEFAULT NULL,
    coinbase_private_key bytea DEFAULT NULL -- private key for coinbase outputs (all 0x00 = not known, but should have one)

    -- Cannot have non-unique constraints
    -- FOREIGN KEY (side_template_id) REFERENCES side_blocks (template_id)
);

-- CLUSTER VERBOSE main_blocks USING main_blocks_height_key;


CREATE TABLE IF NOT EXISTS main_coinbase_outputs (
    id bytea NOT NULL, -- coinbase id
    index int NOT NULL, -- transaction output index
    global_output_index bigint UNIQUE NOT NULL, -- Monero global output idx
    miner bigint NOT NULL, -- owner of the output
    value bigint NOT NULL,
    PRIMARY KEY (id, index),
    FOREIGN KEY (id) REFERENCES main_blocks (coinbase_id),
    FOREIGN KEY (miner) REFERENCES miners (id)
);

CREATE INDEX IF NOT EXISTS main_coinbase_outputs_id_idx ON main_coinbase_outputs (id);
CREATE INDEX IF NOT EXISTS main_coinbase_outputs_miner_idx ON main_coinbase_outputs (miner);

CREATE TABLE IF NOT EXISTS main_likely_sweep_transactions (
    id bytea PRIMARY KEY NOT NULL, -- transaction id
    timestamp bigint NOT NULL, -- when the transaction was made / included in block
    result jsonb NOT NULL, -- MinimalTransactionInputQueryResults
    match jsonb NOT NULL, -- TransactionInputQueryResultsMatch
    value bigint NOT NULL,
    spending_output_indices bigint[] NOT NULL, -- global output indices consumed by this transaction (including decoys)
    global_output_indices bigint[] NOT NULL, -- global output indices produced by this transaction

    input_count integer NOT NULL, -- count of inputs
    input_decoy_count integer NOT NULL, -- count of decoys per input

    miner_count integer NOT NULL,
    other_miners_count integer NOT NULL,
    no_miner_count integer NOT NULL,

    miner_ratio real NOT NULL,
    other_miners_ratio real NOT NULL,
    no_miner_ratio real NOT NULL,

    miner_spend_public_key bytea NOT NULL, -- plausible owner of the transaction
    miner_view_public_key bytea NOT NULL
);
CREATE INDEX IF NOT EXISTS main_likely_sweep_transactions_miner_idx ON main_likely_sweep_transactions (miner_spend_public_key, miner_view_public_key);

CREATE INDEX IF NOT EXISTS main_likely_sweep_transactions_spending_output_indexes_idx ON main_likely_sweep_transactions USING GIN (spending_output_indices);
CREATE INDEX IF NOT EXISTS main_likely_sweep_transactions_global_output_indexes_idx ON main_likely_sweep_transactions USING GIN (global_output_indices);

CREATE EXTENSION IF NOT EXISTS pg_stat_statements;


-- Views

CREATE OR REPLACE VIEW found_main_blocks AS
    SELECT
    m.id AS main_id,
    m.height AS main_height,
    m.timestamp AS timestamp,
    m.reward AS reward,
    m.coinbase_id AS coinbase_id,
    m.coinbase_private_key AS coinbase_private_key,
    m.difficulty AS main_difficulty,
    m.side_template_id AS template_id,
    s.side_height AS side_height,
    s.miner AS miner,
    s.uncle_of AS uncle_of,
    s.effective_height AS effective_height,
    s.window_depth AS window_depth,
    s.window_outputs AS window_outputs,
    s.transaction_count AS transaction_count,
    s.difficulty AS side_difficulty,
    s.cumulative_difficulty AS side_cumulative_difficulty,
    s.inclusion AS side_inclusion
    FROM
        (SELECT * FROM main_blocks WHERE side_template_id IS NOT NULL) AS m
            LEFT JOIN LATERAL
        (SELECT * FROM side_blocks) AS s ON s.main_id = m.id;


CREATE OR REPLACE VIEW payouts AS
    SELECT
    o.miner AS miner,
    m.id AS main_id,
    m.height AS main_height,
    m.timestamp AS timestamp,
    m.coinbase_id AS coinbase_id,
    m.coinbase_private_key AS coinbase_private_key,
    m.side_template_id AS template_id,
    s.side_height AS side_height,
    s.uncle_of AS uncle_of,
    o.value AS value,
    o.index AS index,
    o.global_output_index AS global_output_index,
    s.including_height AS including_height
    FROM
        (SELECT id, value, index, global_output_index, miner FROM main_coinbase_outputs) AS o
            LEFT JOIN LATERAL
        (SELECT id, height, timestamp, side_template_id, coinbase_id, coinbase_private_key FROM main_blocks) AS m ON m.coinbase_id = o.id
            LEFT JOIN LATERAL
        (SELECT template_id, main_id, side_height, uncle_of, (effective_height - window_depth) AS including_height FROM side_blocks) AS s ON s.main_id = m.id;