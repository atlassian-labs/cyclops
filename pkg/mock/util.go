package mock

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
)

func generateRandomInstanceId() (string, error) {
	numBytes := 9
	randomBytes := make([]byte, numBytes)

	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}

	hexString := hex.EncodeToString(randomBytes)
	hexString = hexString[:17]
	return "i-" + hexString, nil
}

func newNodegroup(name string, num int) ([]*Node, error) {
	nodes := make([]*Node, 0)

	for i := 0; i < num; i++ {
		instanceID, err := generateRandomInstanceId()
		if err != nil {
			return nil, err
		}

		node := &Node{
			Name:       fmt.Sprintf("node-%d", i),
			LabelKey:   "customer",
			LabelValue: "kitt",
			Creation:   time.Now(),
			Tainted:    false,
			NodeReady:  corev1.ConditionTrue,
			Nodegroup:  name,
			InstanceID: instanceID,
		}

		nodes = append(nodes, node)
	}

	return nodes, nil
}
