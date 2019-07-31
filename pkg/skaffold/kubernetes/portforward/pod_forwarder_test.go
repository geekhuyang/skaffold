/*
Copyright 2019 The Skaffold Authors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package portforward

import (
	"context"
	"fmt"
	"io/ioutil"
	"reflect"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	fakekubeclientset "k8s.io/client-go/kubernetes/fake"

	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/event"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/kubernetes"
	"github.com/GoogleContainerTools/skaffold/pkg/skaffold/schema/latest"
	"github.com/GoogleContainerTools/skaffold/testutil"
)

func TestAutomaticPortForwardPod(t *testing.T) {
	tests := []struct {
		description     string
		pods            []*v1.Pod
		forwarder       *testForwarder
		expectedPorts   map[int]struct{}
		expectedEntries map[string]*portForwardEntry
		availablePorts  []int
		shouldErr       bool
	}{
		{
			description:    "single container port",
			expectedPorts:  map[int]struct{}{8080: {}},
			availablePorts: []int{8080},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 1,
					podName:         "podname",
					containerName:   "containername",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					portName:               "portname",
					localPort:              8080,
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:   "unavailable container port",
			expectedPorts: map[int]struct{}{9000: {}},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 1,
					podName:         "podname",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					containerName:          "containername",
					portName:               "portname",
					localPort:              9000,
				},
			},
			availablePorts: []int{9000},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:     "bad resource version",
			expectedPorts:   map[int]struct{}{},
			shouldErr:       true,
			expectedEntries: map[string]*portForwardEntry{},
			availablePorts:  []int{8080},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "10000000000a",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:    "forward error",
			expectedPorts:  map[int]struct{}{8080: {}},
			forwarder:      newTestForwarder(fmt.Errorf("")),
			shouldErr:      true,
			availablePorts: []int{8080},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 1,
					podName:         "podname",
					containerName:   "containername",
					portName:        "portname",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					localPort:              8080,
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:    "two different container ports",
			expectedPorts:  map[int]struct{}{8080: {}, 50051: {}},
			availablePorts: []int{8080, 50051},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 1,
					podName:         "podname",
					containerName:   "containername",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					portName:               "portname",
					automaticPodForwarding: true,
					localPort:              8080,
				},
				"containername2-namespace2-portname2-50051": {
					resourceVersion: 1,
					podName:         "podname2",
					containerName:   "containername2",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname2",
						Namespace: "namespace2",
						Port:      50051,
						LocalPort: 50051,
					},
					portName:               "portname2",
					automaticPodForwarding: true,
					localPort:              50051,
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname2",
						ResourceVersion: "1",
						Namespace:       "namespace2",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername2",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 50051,
										Name:          "portname2",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:    "two same container ports",
			expectedPorts:  map[int]struct{}{8080: {}, 9000: {}},
			availablePorts: []int{8080, 9000},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 1,
					podName:         "podname",
					containerName:   "containername",
					portName:        "portname",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					localPort:              8080,
				},
				"containername2-namespace2-portname2-8080": {
					resourceVersion: 1,
					podName:         "podname2",
					containerName:   "containername2",
					portName:        "portname2",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname2",
						Namespace: "namespace2",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					localPort:              9000,
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname2",
						ResourceVersion: "1",
						Namespace:       "namespace2",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername2",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname2",
									},
								},
							},
						},
					},
				},
			},
		},
		{
			description:    "updated pod gets port forwarded",
			expectedPorts:  map[int]struct{}{8080: {}},
			availablePorts: []int{8080},
			expectedEntries: map[string]*portForwardEntry{
				"containername-namespace-portname-8080": {
					resourceVersion: 2,
					podName:         "podname",
					containerName:   "containername",
					portName:        "portname",
					resource: latest.PortForwardResource{
						Type:      "pod",
						Name:      "podname",
						Namespace: "namespace",
						Port:      8080,
						LocalPort: 8080,
					},
					automaticPodForwarding: true,
					localPort:              8080,
				},
			},
			pods: []*v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "1",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:            "podname",
						ResourceVersion: "2",
						Namespace:       "namespace",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name: "containername",
								Ports: []v1.ContainerPort{
									{
										ContainerPort: 8080,
										Name:          "portname",
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			event.InitializeState(latest.BuildConfig{})
			taken := map[int]struct{}{}

			t.Override(&forwardingTimeoutTime, 500*time.Millisecond)
			t.Override(&retrieveAvailablePort, mockRetrieveAvailablePort(taken, test.availablePorts))

			entryManager := EntryManager{
				output:             ioutil.Discard,
				forwardedPorts:     newForwardedPorts(),
				forwardedResources: newForwardedResources(),
			}
			p := NewWatchingPodForwarder(entryManager, kubernetes.NewImageList(), nil)
			if test.forwarder == nil {
				test.forwarder = newTestForwarder(nil)
			}
			p.EntryForwarder = test.forwarder

			for _, pod := range test.pods {
				err := p.portForwardPod(context.Background(), pod)
				t.CheckError(test.shouldErr, err)
			}

			// cmp.Diff cannot access unexported fields, so use reflect.DeepEqual here directly
			if !reflect.DeepEqual(test.expectedPorts, test.forwarder.forwardedPorts.ports) {
				t.Errorf("Expected differs from actual entries. Expected: %v, Actual: %v", test.expectedPorts, test.forwarder.forwardedPorts.ports)
			}

			actualForwardedResources := test.forwarder.forwardedResources.resources
			// cmp.Diff cannot access unexported fields, so use reflect.DeepEqual here directly
			if !reflect.DeepEqual(test.expectedEntries, actualForwardedResources) {
				t.Errorf("Forwarded entries differs from expected entries. Expected: %s, Actual: %v", test.expectedEntries, actualForwardedResources)
			}
		})
	}
}

func TestStartPodForwarder(t *testing.T) {
	tests := []struct {
		description   string
		entryExpected bool
		obj           runtime.Object
		event         watch.EventType
	}{
		{
			description:   "pod modified event",
			entryExpected: true,
			event:         watch.Modified,
		},
		{
			description: "pod error event",
			event:       watch.Error,
		},
		{
			description: "event isn't for a pod",
			obj:         &v1.Service{},
			event:       watch.Modified,
		},
		{
			description: "event is deleted",
			event:       watch.Deleted,
		},
	}

	for _, test := range tests {
		testutil.Run(t, test.description, func(t *testutil.T) {
			event.InitializeState(latest.BuildConfig{})
			client := fakekubeclientset.NewSimpleClientset(&v1.Pod{})
			fakeWatcher := watch.NewRaceFreeFake()
			client.PrependWatchReactor("*", testutil.SetupFakeWatcher(fakeWatcher))

			waitForWatcher := make(chan bool)
			t.Override(&aggregatePodWatcher, func(_ []string, aggregate chan<- watch.Event) (func(), error) {
				go func() {
					waitForWatcher <- true
					for msg := range fakeWatcher.ResultChan() {
						aggregate <- msg
					}
				}()
				return func() {}, nil
			})

			imageList := kubernetes.NewImageList()
			imageList.Add("image")

			p := NewWatchingPodForwarder(NewEntryManager(ioutil.Discard, nil), imageList, nil)
			fakeForwarder := newTestForwarder(nil)
			p.EntryForwarder = fakeForwarder
			p.Start(context.Background())

			// Wait for the watcher to start before we send it an event
			<-waitForWatcher
			obj := test.obj
			if test.obj == nil {
				obj = &v1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Namespace:       "default",
						ResourceVersion: "9",
					},
					Spec: v1.PodSpec{
						Containers: []v1.Container{
							{
								Name:  "mycontainer",
								Image: "image",
								Ports: []v1.ContainerPort{
									{
										Name:          "myport",
										ContainerPort: 8080,
									},
								},
							},
						},
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				}
			}

			fakeWatcher.Action(test.event, obj)

			// wait for the pod resource to be forwarded
			err := wait.PollImmediate(10*time.Millisecond, 100*time.Millisecond, func() (bool, error) {
				_, ok := fakeForwarder.forwardedResources.Load("mycontainer-default-myport-8080")
				return ok, nil
			})
			if err != nil && test.entryExpected {
				t.Fatalf("expected entry wasn't forwarded: %v", err)
			}
		})
	}
}
