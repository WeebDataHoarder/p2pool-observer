#!/bin/bash

SCRIPT_DIR=$( cd -- "$( dirname -- "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )
pushd "${SCRIPT_DIR}"

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
download_compressed_test sidechain_dump.dat
download_compressed_test sidechain_dump_mini.dat
