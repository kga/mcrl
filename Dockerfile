# GoReleaser（dockers_v2）がビルドした mcrl と、そのライセンス表示を入れるだけのイメージ。
# ベースは distroless の static（シェルなし・非 root）を、ダイジェストで固定する。
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG TARGETPLATFORM
COPY $TARGETPLATFORM/mcrl /usr/bin/mcrl
COPY LICENSE THIRD_PARTY_LICENSES /usr/share/doc/mcrl/
ENTRYPOINT ["/usr/bin/mcrl"]
