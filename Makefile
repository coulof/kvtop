BIN := bin/kvtop
PKG := ./...
KUBECONFIG ?= ~/.kube/config

.PHONY: all build test lint record clean

all: build

build:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o $(BIN) ./cmd/kvtop

test:
	go test -v -race $(PKG)

lint:
	go vet $(PKG)

record:
	@mkdir -p testdata
	@echo "Recording virt-handler metrics to testdata/..."
	@KUBECONFIG=$(KUBECONFIG) kubectl get pods -n harvester-system -l kubevirt.io=virt-handler -o jsonpath='{range .items[*]}{.metadata.name}{" "}{.spec.nodeName}{"\n"}{end}' | while read pod node; do \
		echo "Scraping $$pod on $$node..."; \
		KUBECONFIG=$(KUBECONFIG) kubectl get --raw "/api/v1/namespaces/harvester-system/pods/https:$$pod:8443/proxy/metrics" > "testdata/virt-handler-$$node.prom"; \
	done
	@echo "Scraping node metrics from metrics-server..."
	@KUBECONFIG=$(KUBECONFIG) kubectl get --raw "/apis/metrics.k8s.io/v1beta1/nodes" > "testdata/metrics-server-nodes.json"
	@echo "Scraping VMI list..."
	@KUBECONFIG=$(KUBECONFIG) kubectl get vmi -A -o json > "testdata/vmis.json"
	@echo "Done recording fixtures to testdata/"

clean:
	rm -rf bin/
