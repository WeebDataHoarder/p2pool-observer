CREATE TABLE miners (
    id bigint PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
    alias varchar UNIQUE DEFAULT NULL,
    spend_public_key bytea NOT NULL,
    view_public_key bytea NOT NULL,
    UNIQUE (spend_public_key, view_public_key)
);


CREATE TABLE side_blocks (
    main_id bytea PRIMARY KEY, -- mainchain id, on Monero network
    main_height bigint NOT NULL, -- mainchain height

    template_id bytea NOT NULL, -- sidechain template id. Note multiple blocks can exist per template id, see inclusion
    side_height bigint NOT NULL, -- sidechain height
    parent_template_id varchar UNIQUE NOT NULL, -- previous sidechain template id

    miner bigint NOT NULL, -- miner who contributed the block

    -- uncle inclusion information
    uncle_of bytea DEFAULT NULL, -- has been included under this parent block template id as an uncle
    effective_height bigint NOT NULL, -- has been included under this parent block height as an uncle, or is this height


    -- other not indexed data

    timestamp bigint NOT NULL, -- mainchain timestamp
    software_id int NOT NULL, -- Software used to generate this template
    software_version int NOT NULL, -- Software version used to generate this template
    window_height int NOT NULL, -- PPLNS window depth, in blocks including this one
    window_outputs int NOT NULL, -- number of outputs on coinbase transaction
    transaction_count int NOT NULL, -- number of transactions included in the template

    difficulty bigint NOT NULL, -- sidechain difficulty at height
    pow_difficulty bigint NOT NULL, -- difficulty of pow_hash
    pow_hash bytea NOT NULL, -- result of PoW function as a hash (all 0x00 = not known)

    inclusion int NOT NULL DEFAULT 1, -- how the block is included:
    -- 0 = orphan (was not included in-verified-chain)
    -- 1 = in-verified-chain (uncle or main)
    -- 2 = alternate in-verified-chain (uncle or main), for example when duplicate nonce happens
    -- Higher values might specify forks or other custom additions

    FOREIGN KEY (uncle_of) REFERENCES side_blocks (template_id),
    FOREIGN KEY (uncle_of_height) REFERENCES side_blocks (side_height),
    FOREIGN KEY (miner) REFERENCES miners (id)
);

CREATE INDEX side_blocks_miner_idx ON side_blocks (miner);
CREATE INDEX side_blocks_template_id_idx ON side_blocks (template_id);
CREATE INDEX side_blocks_main_height_idx ON side_blocks (main_height);
CREATE INDEX side_blocks_side_height_idx ON side_blocks (side_height);
CREATE INDEX side_blocks_parent_template_id_idx ON side_blocks (parent_template_id);
CREATE INDEX side_blocks_uncle_of_idx ON side_blocks (uncle_of);
CREATE INDEX side_blocks_effective_height_idx ON side_blocks (effective_height);


CREATE TABLE main_blocks (
    main_id bytea PRIMARY KEY,
    main_height bigint UNIQUE NOT NULL,
    timestamp bigint NOT NULL, -- timestamp as set in block
    reward bigint NOT NULL,
    coinbase_id bytea UNIQUE NOT NULL,
    difficulty bigint NOT NULL, -- mainchain difficulty at height
    metadata jsonb DEFAULT NULL, -- metadata such as pool ownership, links to other p2pool networks, and other interesting data
    -- sidechain data for blocks who we own
    side_template_id bytea UNIQUE DEFAULT NULL,
    coinbase_private_key bytea DEFAULT NULL, -- private key for coinbase outputs (all 0x00 = not known, but should have one)

    FOREIGN KEY (side_template_id) REFERENCES side_blocks (template_id),
);

CREATE INDEX main_blocks_coinbase_id_idx ON main_blocks (coinbase_id);
CREATE INDEX main_blocks_side_template_id_idx ON main_blocks (side_template_id);


CREATE TABLE main_coinbase_outputs (
    id bytea NOT NULL, -- coinbase id
    index int NOT NULL, -- transaction output index
    global_output_index bigint NOT NULL, -- Monero global output idx
    miner bigint NOT NULL, -- owner of the output
    value bigint NOT NULL,
    PRIMARY KEY (id, index),
    FOREIGN KEY (id) REFERENCES main_blocks (coinbase_id),
    FOREIGN KEY (miner) REFERENCES miners (id)
);
CREATE INDEX main_coinbase_outputs_id_idx ON main_coinbase_outputs (id);
CREATE INDEX main_coinbase_outputs_miner_idx ON main_coinbase_outputs (miner);

-- TODO, maybe also use intarray module
-- CREATE TABLE main_coinbase_sweep_transactions (
--    id bytea NOT NULL, -- transaction id
--    indexes bigint[] NOT NULL, -- Monero global output indexes used
--    miner bigint NOT NULL, -- plausible owner of the transaction
--    -- not possible yet in postgres FOREIGN KEY (EACH ELEMENT OF indexes) REFERENCES main_coinbase_outputs (global_output_index),
--    FOREIGN KEY (miner) REFERENCES miners (id)
--);
--CREATE INDEX main_coinbase_sweep_transactions_indexes_idx ON main_coinbase_outputs USING GIN (indexes);