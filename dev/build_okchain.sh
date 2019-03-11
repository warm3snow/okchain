set -x
set -e

REPO_NAME=ok-chain/okchain
PROJECT=${GOPATH}/src/github.com/${REPO_NAME}
SRC_DIR=${PROJECT}/cmd
BIN_DIR=${PROJECT}/build/bin

function build {
    if [ ! -f ${BIN_DIR} ]; then
        cd ${PROJECT}
        mkdir -p build/bin
    fi
    cd ${SRC_DIR}/okchaind; GOBIN=${PROJECT}/build/bin go install
    cd ${SRC_DIR}/okchaincli; GOBIN=${PROJECT}/build/bin go install
}

build
