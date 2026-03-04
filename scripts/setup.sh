#!/usr/bin/env bash
# TOL Chain 단일 노드 초기 설정 스크립트
# 서버에서 한 번만 실행하면 됩니다.
set -euo pipefail

echo "=== TOL Chain 단일 노드 배포 설정 ==="

# Docker 설치 확인
command -v docker >/dev/null 2>&1 || { echo "오류: Docker가 설치되지 않았습니다."; exit 1; }

# ── 1. .env 파일 생성 ────────────────────────────────────────────
if [ ! -f .env ]; then
    read -rsp "키스토어 비밀번호 입력 (숨김 처리): " TOL_PASSWORD; echo
    [ -n "$TOL_PASSWORD" ] || { echo "오류: 비밀번호는 비워둘 수 없습니다."; exit 1; }
    printf 'TOL_PASSWORD=%s\n' "$TOL_PASSWORD" > .env
    chmod 600 .env
    echo ".env 파일 생성됨 (권한 600)"
fi
set -a; source .env; set +a
: "${TOL_PASSWORD:?TOL_PASSWORD가 .env에 설정되어야 합니다}"

# ── 2. Docker 이미지 빌드 ────────────────────────────────────────
echo ""
echo "Docker 이미지 빌드 중..."
docker build -t tolchain-node:latest .

# ── 3. 검증자 키 생성 ────────────────────────────────────────────
PUBKEY=""
if [ ! -f validator.key ]; then
    echo ""
    echo "검증자 키 생성 중..."
    TMPOUT=$(mktemp)

    CID=$(docker create -e TOL_PASSWORD tolchain-node:latest --genkey --key validator.key)
    docker start -a "$CID" 2>&1 | tee "$TMPOUT"
    docker cp "$CID:/home/tol/validator.key" ./validator.key
    docker rm "$CID" >/dev/null

    PUBKEY=$(grep "Public key" "$TMPOUT" | awk '{print $NF}')
    rm -f "$TMPOUT"

    [ -n "$PUBKEY" ] || { echo "오류: 공개 키 추출 실패"; exit 1; }
    chmod 600 validator.key
    echo "검증자 공개 키: $PUBKEY"
else
    echo "validator.key 이미 존재. 키 생성 건너뜀."
fi

# ── 4. config.json 생성 ──────────────────────────────────────────
if [ ! -f config.json ]; then
    if [ -z "$PUBKEY" ]; then
        echo ""
        echo "오류: config.json이 없고 공개 키를 알 수 없습니다."
        echo "아래 형식으로 config.json을 직접 작성하세요:"
        cat <<'EXAMPLE'
{
  "node_id": "node0",
  "data_dir": "./data",
  "rpc_port": 8545,
  "p2p_port": 30303,
  "max_block_txs": 500,
  "validators": ["<검증자_공개키_64자리_hex>"],
  "genesis": {
    "chain_id": "tolchain-dev",
    "alloc": { "<검증자_공개키_64자리_hex>": 1000000 }
  }
}
EXAMPLE
        exit 1
    fi

    cat > config.json <<EOF
{
  "node_id": "node0",
  "data_dir": "./data",
  "rpc_port": 8545,
  "p2p_port": 30303,
  "max_block_txs": 500,
  "validators": ["${PUBKEY}"],
  "genesis": {
    "chain_id": "tolchain-dev",
    "alloc": {
      "${PUBKEY}": 1000000
    }
  }
}
EOF
    echo "config.json 생성됨"
fi

mkdir -p data

# ── 완료 ─────────────────────────────────────────────────────────
echo ""
echo "=== 설정 완료 ==="
echo ""
echo "노드 시작:     docker compose up -d"
echo "로그 확인:     docker compose logs -f"
echo "노드 중지:     docker compose down"
echo ""
echo "RPC 테스트:"
echo "  curl -s -X POST http://localhost:8545 \\"
echo "    -H 'Content-Type: application/json' \\"
echo "    -d '{\"jsonrpc\":\"2.0\",\"method\":\"getBlockHeight\",\"id\":1}'"
