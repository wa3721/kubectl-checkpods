MINIKUBE_CONTAINER_ID := $(shell docker ps --filter "status=running" --format "{{.ID}}")
KUBE_DIR := ./profiles/minikube

.PHONY: kubeconfig-init
kubeconfig-init:
	docker cp $(shell docker ps --filter "status=running" --format "{{.ID}}"):/root/export-kube/config ./minikube-config

.PHONY: minikube-init kubectl-test

minikube-init:
	@echo "==> Copy kubeconfig from devcontainer"
	@rm -rf $(KUBE_DIR)
	@mkdir -p $(KUBE_DIR)
	docker cp $(MINIKUBE_CONTAINER_ID):/root/export-kube/. $(KUBE_DIR)

	@echo "==> Fix kubeconfig paths (relative)"
	sed -i \
		-e 's|/root/.minikube/ca.crt|ca.crt|g' \
		-e 's|/root/.minikube/profiles/minikube/client.crt|client.crt|g' \
		-e 's|/root/.minikube/profiles/minikube/client.key|client.key|g' \
		$(KUBE_DIR)/config

	@echo "modify apiserver address to localhost"
	sed -i \
        -e 's|https://192.168.49.2:8443|https://localhost:33333|g' \
        $(KUBE_DIR)/config

	@echo "==> Kubeconfig ready at $(KUBE_DIR)/config"

kubectl-test:
	kubectl --kubeconfig=$(KUBE_DIR)/config get nodes

.PHONY: kubeconfig-cp
kubeconfig-cp:
	cp -af $(KUBE_DIR)/config /root/.kube/minikube.yaml
	cp -af $(KUBE_DIR)/. /root/.kube/

.PHONY: switch-dev
switch-dev:
	kctx minikube