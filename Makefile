

pack:
	pack build \
      --builder paketobuildpacks/builder-jammy-buildpackless-static \
      --buildpack paketo-buildpacks/go \
      --env "CGO_ENABLED=0" \
      --env "BP_GO_BUILD_FLAGS=-buildmode=default" \
      --timestamps \
      --tag joostvdgtanzu/go-demo-proxy:0.5.0 \
      go-demo-proxy