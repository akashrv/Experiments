package main

import (
	"context"
	"fmt"
	"log"

	"cloud.google.com/go/pubsub"
	"cloud.google.com/go/storage"
	vision "cloud.google.com/go/vision/apiv1"
	cloudevents "github.com/cloudevents/sdk-go"
	visionpb "google.golang.org/genproto/googleapis/cloud/vision/v1"
)

var (
	finalizeEventType = "com.google.storage.finalize"
)

type labelWithConfidenceScore struct {
	Label      string
	Confidence int
}

func (lwcs *labelWithConfidenceScore) String() string {
	return fmt.Sprintf("Label:%s Confidence:%d%%", lwcs.Label, lwcs.Confidence)
}

func processEvent(event cloudevents.Event) {
	if event.Context == nil {
		fmt.Printf("event.Context is nil. cloudevents.Event\n%s\n", event.String())
		return
	}
	if event.Context.GetType() != finalizeEventType {
		fmt.Printf("Invalid event type %s. Supported event type: %s\n", event.Context.GetType(), finalizeEventType)
		return
	}
	pubsubMsg := pubsub.Message{}
	if err := event.DataAs(&pubsubMsg); err != nil {
		fmt.Printf("Error extracting data from event. Error:%s\n", err.Error())
		return
	}
	bucketID, err := getBucketID(&pubsubMsg)
	if err != nil {
		fmt.Printf(err.Error())
		return
	}
	objectID, err := getobjectID(&pubsubMsg)
	if err != nil {
		fmt.Printf(err.Error())
		return
	}
	objectURI := getObjectURI(bucketID, objectID)
	// fmt.Printf("ObjectURI:%s\n", objectURI)

	labels, err := extractLabels(objectURI)
	if err != nil {
		fmt.Printf("Error extracting extracting labels. Error:%s", err.Error())
		return
	}

	fmt.Printf("I am %d%% confident that you uploaded an image of a %q @%s", labels[0].Confidence, labels[0].Label, objectURI)

	if err := writeToGcs(bucketID, objectID+".attributes.txt", labels); err != nil {
		fmt.Printf(err.Error())
		return
	}
}
func getBucketID(pubsubMsg *pubsub.Message) (string, error) {
	return getAttribute("bucketId", pubsubMsg)
}
func getobjectID(pubsubMsg *pubsub.Message) (string, error) {
	return getAttribute("objectId", pubsubMsg)
}

func getAttribute(attribute string, pubsubMsg *pubsub.Message) (string, error) {
	if pubsubMsg == nil {
		return "", fmt.Errorf("pubsubMsg is nil")
	}
	attributeValue, ok := pubsubMsg.Attributes[attribute]
	if !ok {
		return "", fmt.Errorf("%s not found in Data.Attributes", attribute)
	}
	return attributeValue, nil
}

func getObjectURI(bucketID, objectID string) string {
	return fmt.Sprintf("gs://%s/%s", bucketID, objectID)
}

func writeToGcs(bucketID string, objectID string, labels []labelWithConfidenceScore) error {
	ctx := context.Background()
	client, err := storage.NewClient(ctx)
	if err != nil {
		return err
	}
	wc := client.Bucket(bucketID).Object(objectID).NewWriter(ctx)

	for _, label := range labels {
		fmt.Fprintln(wc, label.String())
	}

	if err := wc.Close(); err != nil {
		return err
	}

	return nil
}

func main() {

	//	 customEvent := cloudevents.NewEvent(cloudevents.VersionV02)

	//	 customEvent.SetType("com.google.storage.finalize")
	//	 customEvent.SetData(pubsub.Message{
	//	 	Attributes: map[string]string{
	//	 		"bucketId": "akashv-bucket",
	//	 		"objectId": "images/cat.jpg",
	//	 	},
	//	 })
	//	 processEvent(customEvent)

	c, err := cloudevents.NewDefaultClient()
	if err != nil {
		log.Fatal("Failed to create client, ", err)
	}
	log.Fatal(c.StartReceiver(context.Background(), processEvent))

}

func extractLabels(objectURI string) ([]labelWithConfidenceScore, error) {
	ctx := context.Background()
	client, err := vision.NewImageAnnotatorClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("Failed to create client: %v", err)
	}
	defer client.Close()

	image := vision.NewImageFromURI(objectURI)
	localizedObjectAnnotations, err := client.LocalizeObjects(ctx, image, nil)
	if err != nil {
		return nil, fmt.Errorf("Failed to get localized object annotations: %v", err)
	}
	return tolabelWithConfidenceScore(localizedObjectAnnotations), nil
}

func tolabelWithConfidenceScore(locObjAnnots []*visionpb.LocalizedObjectAnnotation) []labelWithConfidenceScore {
	labels := make([]labelWithConfidenceScore, len(locObjAnnots))
	for idx, ann := range locObjAnnots {
		labels[idx].Label = ann.Name
		labels[idx].Confidence = int(ann.Score * 100)
	}
	return labels
}
