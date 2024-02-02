
KUSTOMIZE=go run sigs.k8s.io/kustomize/kustomize/v5@latest

deploy:
	cd ${DEPLOYMENT} && $(KUSTOMIZE) edit set image textile/basin_w3s=${IMAGE_BASIN_W3S}
	cd ${DEPLOYMENT} && $(KUSTOMIZE) build . | kubectl apply -f -
.PHONY: deploy
