# syntax=docker/dockerfile:1@sha256:87999aa3d42bdc6bea60565083ee17e86d1f3339802f543c0d03998580f9cb89
ARG ALPINE_REF=alpine@sha256:28bd5fe8b56d1bd048e5babf5b10710ebe0bae67db86916198a6eec434943f8b
ARG UBUNTU_REF=ubuntu:24.04@sha256:4fbb8e6a8395de5a7550b33509421a2bafbc0aab6c06ba2cef9ebffbc7092d90
ARG NODE_REF=node:18@sha256:c6ae79e38498325db67193d391e6ec1d224d96c693a8a4d943498556716d3783
ARG CONTAINER_TEMPLATE_REF=ghcr.io/spr-networks/container_template@sha256:869ada7b121e9a0c552674042d32e801da3c4d04145638d9e722918c6377e65f
ARG SOURCE_DATE_EPOCH

FROM ${ALPINE_REF} AS cacerts

FROM ${UBUNTU_REF} AS builder
ENV DEBIAN_FRONTEND=noninteractive
ARG UBUNTU_SNAPSHOT=20260601T000000Z
ARG GO_VERSION=1.25.12
ARG GO_SHA256_AMD64=234828b7a89e0e303d2556310ee549fbcf253d28de937bac3da13d6294262ac1
ARG GO_SHA256_ARM64=8b5884aef89600aef5b0b051fb971f11f49bb996521e911f30f02a66884f7bd2
ARG ALGO_COMMIT=954856e40197e5ab923203f66f34ea6d7ef78f19
ARG COLLECTION_CRYPTO_VERSION=3.1.1
ARG COLLECTION_CRYPTO_SHA256=0294846a93e2b0f385a54656a17a4f714f2c2d09f48132c24ffc0251e24da54a
ARG COLLECTION_GENERAL_VERSION=11.1.0
ARG COLLECTION_GENERAL_SHA256=05fdb0c466b04da2976374d0cf53fd959babf0ae344a4932072504764b426107
ARG TARGETARCH
COPY --from=cacerts /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
RUN set -eux; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: noble noble-updates noble-security\nComponents: main restricted universe multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' "${UBUNTU_SNAPSHOT}" > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'APT::Install-Recommends "false";\nAcquire::Check-Valid-Until "false";\n' > /etc/apt/apt.conf.d/99reproducible
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git wget python3 python3-venv && rm -rf /var/lib/apt/lists/* /var/log/* /var/cache/ldconfig/aux-cache
RUN set -eux; \
    case "${TARGETARCH}" in \
      amd64) GO_SHA256="${GO_SHA256_AMD64}";; \
      arm64) GO_SHA256="${GO_SHA256_ARM64}";; \
      *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1;; \
    esac; \
    wget -q "https://dl.google.com/go/go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    echo "${GO_SHA256}  go${GO_VERSION}.linux-${TARGETARCH}.tar.gz" | sha256sum -c -; \
    tar -C /usr/local -xzf "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"; \
    rm "go${GO_VERSION}.linux-${TARGETARCH}.tar.gz"
ENV PATH="/usr/local/go/bin:${PATH}" GOTOOLCHAIN=local

# Vendor trailofbits/algo at a pinned full commit hash and verify it.
RUN set -eux; \
    git init /algo-src; \
    cd /algo-src; \
    git remote add origin https://github.com/trailofbits/algo.git; \
    git fetch --depth 1 origin "${ALGO_COMMIT}"; \
    git checkout --detach FETCH_HEAD; \
    test "$(git rev-parse HEAD)" = "${ALGO_COMMIT}"; \
    printf '%s\n' "${ALGO_COMMIT}" > .algo-commit; \
    rm -rf .git configs

# Python venv with fully pinned, hash-checked wheels (see requirements-algo.txt;
# versions come from algo's own lockfile at ALGO_COMMIT).
COPY requirements-algo.txt /tmp/requirements-algo.txt
RUN set -eux; \
    python3 -m venv /opt/algo-venv; \
    /opt/algo-venv/bin/pip install --no-cache-dir --no-compile --require-hashes --no-deps --only-binary=:all: -r /tmp/requirements-algo.txt

# Ansible Galaxy collections pinned per algo's requirements.yml, verified by sha256.
# The remaining collections algo's DigitalOcean path needs ship inside the ansible
# pip package (community.digitalocean 1.27.0, ansible.posix 2.1.0, ansible.utils 6.0.0).
RUN set -eux; \
    wget -q -O /tmp/community-crypto.tar.gz "https://galaxy.ansible.com/api/v3/plugin/ansible/content/published/collections/artifacts/community-crypto-${COLLECTION_CRYPTO_VERSION}.tar.gz"; \
    echo "${COLLECTION_CRYPTO_SHA256}  /tmp/community-crypto.tar.gz" | sha256sum -c -; \
    wget -q -O /tmp/community-general.tar.gz "https://galaxy.ansible.com/api/v3/plugin/ansible/content/published/collections/artifacts/community-general-${COLLECTION_GENERAL_VERSION}.tar.gz"; \
    echo "${COLLECTION_GENERAL_SHA256}  /tmp/community-general.tar.gz" | sha256sum -c -; \
    /opt/algo-venv/bin/ansible-galaxy collection install --offline -p /usr/share/ansible/collections /tmp/community-crypto.tar.gz /tmp/community-general.tar.gz; \
    rm /tmp/community-crypto.tar.gz /tmp/community-general.tar.gz

WORKDIR /code
COPY code/ /code/
RUN --mount=type=tmpfs,target=/root/go/ go build -trimpath -ldflags "-s -w" -o /algo_plugin /code/

FROM ${NODE_REF} AS builder-ui
WORKDIR /app
COPY frontend ./
RUN --mount=type=tmpfs,target=/root/.cache \
    --mount=type=tmpfs,target=/app/node_modules \
    yarn install --frozen-lockfile --network-timeout 86400000 && yarn run bundle

FROM ${CONTAINER_TEMPLATE_REF}
ENV DEBIAN_FRONTEND=noninteractive
ARG UBUNTU_SNAPSHOT=20260601T000000Z
RUN set -eux; \
    rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*; \
    printf 'Types: deb\nURIs: https://snapshot.ubuntu.com/ubuntu/%s\nSuites: noble noble-updates noble-security\nComponents: main restricted universe multiverse\nSigned-By: /usr/share/keyrings/ubuntu-archive-keyring.gpg\n' "${UBUNTU_SNAPSHOT}" > /etc/apt/sources.list.d/ubuntu.sources; \
    printf 'APT::Install-Recommends "false";\nAcquire::Check-Valid-Until "false";\n' > /etc/apt/apt.conf.d/99reproducible
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates python3 openssh-client openssl rsync && rm -rf /var/lib/apt/lists/* /var/log/* /var/cache/ldconfig/aux-cache
# No .pyc writes at runtime (keeps the image layer content the only python state)
ENV PYTHONDONTWRITEBYTECODE=1
COPY --from=builder /algo-src /algo
COPY --from=builder /opt/algo-venv /opt/algo-venv
COPY --from=builder /usr/share/ansible/collections /usr/share/ansible/collections
COPY scripts /scripts/
# Minimal `uv` shim: algo's main.yml shells out to `uv pip ...` for its
# preflight checks; the real deps are baked into /opt/algo-venv at build time.
COPY --chmod=0755 scripts/uv-shim.sh /usr/local/bin/uv
COPY --from=builder /algo_plugin /
COPY --from=builder-ui /app/build/ /ui/

ENTRYPOINT ["/scripts/startup.sh"]
