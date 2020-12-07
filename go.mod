module github.com/maruina/argocd-progressive-rollout-controller

go 1.13

require (
	github.com/argoproj/argo-cd v1.7.10
	github.com/argoproj/gitops-engine v0.1.3-0.20200925215903-d25b8fd69f0d
	github.com/go-logr/logr v0.3.0
	github.com/go-logr/zapr v0.3.0 // indirect
	github.com/onsi/ginkgo v1.12.1
	github.com/onsi/gomega v1.10.1
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.16.0 // indirect
	k8s.io/api v0.18.8
	k8s.io/apimachinery v0.18.8
	k8s.io/client-go v11.0.1-0.20190816222228-6d55c1b1f1ca+incompatible
	sigs.k8s.io/controller-runtime v0.6.4
)

replace k8s.io/api => k8s.io/api v0.18.8

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.18.8

replace k8s.io/apimachinery => k8s.io/apimachinery v0.18.9-rc.0

replace k8s.io/apiserver => k8s.io/apiserver v0.18.8

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.18.8

replace k8s.io/client-go => k8s.io/client-go v0.18.8

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.18.8

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.18.8

replace k8s.io/code-generator => k8s.io/code-generator v0.18.13-rc.0

replace k8s.io/component-base => k8s.io/component-base v0.18.8

replace k8s.io/cri-api => k8s.io/cri-api v0.18.10-rc.0

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.18.8

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.18.8

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.18.8

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.18.8

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.18.8

replace k8s.io/kubectl => k8s.io/kubectl v0.18.8

replace k8s.io/kubelet => k8s.io/kubelet v0.18.8

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.18.8

replace k8s.io/metrics => k8s.io/metrics v0.18.8

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.18.8
