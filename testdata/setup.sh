#!/bin/bash

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
pushd "${SCRIPT_DIR}"

# Pre-v2 p2pool hardfork
OLD_TESTS_COMMIT_ID=b9eb66e2b3e02a5ec358ff8a0c5169a5606d9fde

function download_old_test() {
    if [ -f "./old_${1}" ]; then
      return
    fi
    curl --progress-bar --output "./old_${1}" "https://raw.githubusercontent.com/SChernykh/p2pool/${OLD_TESTS_COMMIT_ID}/tests/src/${1}"
}

TESTS_COMMIT_ID=f455ce398c20137a92a67b062c6311580939abea

function download_test() {
    if [ -f "./${1}" ]; then
      return
    fi
    curl --progress-bar --output "./${1}" "https://raw.githubusercontent.com/SChernykh/p2pool/${TESTS_COMMIT_ID}/tests/src/${1}"
}

function download_compressed_test() {
    if [ -f "./${1}" ]; then
      return
    fi
    curl --progress-bar --output "./${1}.gz" "https://raw.githubusercontent.com/SChernykh/p2pool/${TESTS_COMMIT_ID}/tests/src/${1}.gz" && gzip --decompress "${1}.gz"

}

download_test block.dat
download_test crypto_tests.txt
download_compressed_test sidechain_dump.dat
download_compressed_test sidechain_dump_mini.dat

download_old_test mainnet_test2_block.dat
download_old_test sidechain_dump.dat
download_old_test sidechain_dump_mini.dat
