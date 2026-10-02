package kube

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic/dynamicinformer"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/tools/cache"
)

var vmimGVR = schema.GroupVersionResource{
	Group:    "kubevirt.io",
	Version:  "v1",
	Resource: "virtualmachineinstancemigrations",
}

// MigrationInfo captures the live migration state of a VMI.
type MigrationInfo struct {
	Namespace     string
	VMIName       string
	MigrationName string
	Phase         string
	SourceNode    string
	TargetNode    string
	Active        bool
}

// NodeInfo captures node capacity and readiness.
type NodeInfo struct {
	Name            string
	AllocatableMem  int64
	AllocatableCPUs int64
	Ready           bool
}

// InformerEventHandler receives real-time Kubernetes lifecycle events.
type InformerEventHandler interface {
	OnVMIUpdated(vmi VMIInfo)
	OnVMIDeleted(namespace, name string)
	OnMigrationUpdated(mig MigrationInfo)
	OnMigrationDeleted(namespace, name string)
	OnNodeUpdated(node NodeInfo)
	OnNodeDeleted(nodeName string)
	OnNamespaceUpdated(namespace string)
	OnNamespaceDeleted(namespace string)
}

// InformerManager manages informer factories for VMI, VMIM, Node, and Namespace.
type InformerManager struct {
	client         *Client
	handler        InformerEventHandler
	dynamicFactory dynamicinformer.DynamicSharedInformerFactory
	typedFactory   informers.SharedInformerFactory
	stopCh         chan struct{}
}

// NewInformerManager creates informers watching VMI, VMIM, Node, and Namespace.
func NewInformerManager(client *Client, handler InformerEventHandler, resyncPeriod time.Duration) *InformerManager {
	if resyncPeriod <= 0 {
		resyncPeriod = 10 * time.Minute
	}

	dynFactory := dynamicinformer.NewDynamicSharedInformerFactory(client.Dynamic, resyncPeriod)
	typedFactory := informers.NewSharedInformerFactory(client.Clientset, resyncPeriod)

	im := &InformerManager{
		client:         client,
		handler:        handler,
		dynamicFactory: dynFactory,
		typedFactory:   typedFactory,
		stopCh:         make(chan struct{}),
	}

	im.setupVMIInformer()
	im.setupVMIMInformer()
	im.setupNodeInformer()
	im.setupNamespaceInformer()

	return im
}

func (im *InformerManager) setupVMIInformer() {
	informer := im.dynamicFactory.ForResource(vmiGVR).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				im.handler.OnVMIUpdated(ExtractVMIInfo(u))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if u, ok := newObj.(*unstructured.Unstructured); ok {
				im.handler.OnVMIUpdated(ExtractVMIInfo(u))
			}
		},
		DeleteFunc: func(obj interface{}) {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				im.handler.OnVMIDeleted(u.GetNamespace(), u.GetName())
			}
		},
	})
}

func (im *InformerManager) setupVMIMInformer() {
	informer := im.dynamicFactory.ForResource(vmimGVR).Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				im.handler.OnMigrationUpdated(ExtractMigrationInfo(u))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if u, ok := newObj.(*unstructured.Unstructured); ok {
				im.handler.OnMigrationUpdated(ExtractMigrationInfo(u))
			}
		},
		DeleteFunc: func(obj interface{}) {
			if u, ok := obj.(*unstructured.Unstructured); ok {
				im.handler.OnMigrationDeleted(u.GetNamespace(), u.GetName())
			}
		},
	})
}

func (im *InformerManager) setupNodeInformer() {
	informer := im.typedFactory.Core().V1().Nodes().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if node, ok := obj.(*corev1.Node); ok {
				im.handler.OnNodeUpdated(extractNodeInfo(node))
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if node, ok := newObj.(*corev1.Node); ok {
				im.handler.OnNodeUpdated(extractNodeInfo(node))
			}
		},
		DeleteFunc: func(obj interface{}) {
			if node, ok := obj.(*corev1.Node); ok {
				im.handler.OnNodeDeleted(node.Name)
			}
		},
	})
}

func (im *InformerManager) setupNamespaceInformer() {
	informer := im.typedFactory.Core().V1().Namespaces().Informer()
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			if ns, ok := obj.(*corev1.Namespace); ok {
				im.handler.OnNamespaceUpdated(ns.Name)
			}
		},
		UpdateFunc: func(oldObj, newObj interface{}) {
			if ns, ok := newObj.(*corev1.Namespace); ok {
				im.handler.OnNamespaceUpdated(ns.Name)
			}
		},
		DeleteFunc: func(obj interface{}) {
			if ns, ok := obj.(*corev1.Namespace); ok {
				im.handler.OnNamespaceDeleted(ns.Name)
			}
		},
	})
}

// Start initiates watching and starts all informer goroutines.
func (im *InformerManager) Start(ctx context.Context) error {
	im.dynamicFactory.Start(im.stopCh)
	im.typedFactory.Start(im.stopCh)

	syncCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if !cache.WaitForCacheSync(syncCtx.Done(),
		im.dynamicFactory.ForResource(vmiGVR).Informer().HasSynced,
		im.dynamicFactory.ForResource(vmimGVR).Informer().HasSynced,
		im.typedFactory.Core().V1().Nodes().Informer().HasSynced,
		im.typedFactory.Core().V1().Namespaces().Informer().HasSynced,
	) {
		return fmt.Errorf("informer caches failed to sync")
	}

	return nil
}

// Stop shuts down all informers.
func (im *InformerManager) Stop() {
	close(im.stopCh)
}

// ExtractMigrationInfo parses unstructured VirtualMachineInstanceMigration into MigrationInfo.
func ExtractMigrationInfo(u *unstructured.Unstructured) MigrationInfo {
	ns := u.GetNamespace()
	migName := u.GetName()
	vmiName, _, _ := unstructured.NestedString(u.Object, "spec", "vmiName")
	phase, _, _ := unstructured.NestedString(u.Object, "status", "phase")

	completed, _, _ := unstructured.NestedBool(u.Object, "status", "migrationState", "completed")
	failed, _, _ := unstructured.NestedBool(u.Object, "status", "migrationState", "failed")
	srcNode, _, _ := unstructured.NestedString(u.Object, "status", "migrationState", "sourceNode")
	tgtNode, _, _ := unstructured.NestedString(u.Object, "status", "migrationState", "targetNode")

	active := !completed && !failed && phase != "Succeeded" && phase != "Failed" && phase != ""

	return MigrationInfo{
		Namespace:     ns,
		VMIName:       vmiName,
		MigrationName: migName,
		Phase:         phase,
		SourceNode:    srcNode,
		TargetNode:    tgtNode,
		Active:        active,
	}
}

func extractNodeInfo(node *corev1.Node) NodeInfo {
	memAlloc := node.Status.Allocatable.Memory().Value()
	cpuAlloc := node.Status.Allocatable.Cpu().MilliValue() / 1000

	ready := false
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady && c.Status == corev1.ConditionTrue {
			ready = true
			break
		}
	}

	return NodeInfo{
		Name:            node.Name,
		AllocatableMem:  memAlloc,
		AllocatableCPUs: cpuAlloc,
		Ready:           ready,
	}
}
