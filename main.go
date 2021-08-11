package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/slack-go/slack/socketmode"
)

func main() {

	// Load Env variables from .dot file
	godotenv.Load(".env")

	token := os.Getenv("SLACK_AUTH_TOKEN")
	appToken := os.Getenv("SLACK_APP_TOKEN")
	// Create a new client to slack by giving token
	// Set debug to true while developing
	// Also add a ApplicationToken option to the client
	client := slack.New(token, slack.OptionDebug(true), slack.OptionAppLevelToken(appToken))
	// go-slack comes with a SocketMode package that we need to use that accepts a Slack client and outputs a Socket mode client instead
	socketClient := socketmode.New(
		client,
		socketmode.OptionDebug(true),
		// Option to set a custom logger
		socketmode.OptionLog(log.New(os.Stdout, "socketmode: ", log.Lshortfile|log.LstdFlags)),
	)

	// Create a context that can be used to cancel goroutine
	ctx, cancel := context.WithCancel(context.Background())
	// Make this cancel called properly in a real program , graceful shutdown etc
	defer cancel()

	go func(ctx context.Context, client *slack.Client, socketClient *socketmode.Client) {
		// Create a for loop that selects either the context cancellation or the events incomming
		for {
			select {
			// inscase context cancel is called exit the goroutine
			case <-ctx.Done():
				log.Println("Shutting down socketmode listener")
				return
			case event := <-socketClient.Events:
				// We have a new Events, let's type switch the event
				// Add more use cases here if you want to listen to other events.
				switch event.Type {
				// handle EventAPI events
				case socketmode.EventTypeEventsAPI:
					// The Event sent on the channel is not the same as the EventAPI events so we need to type cast it
					eventsAPIEvent, ok := event.Data.(slackevents.EventsAPIEvent)
					if !ok {
						log.Printf("Could not type cast the event to the EventsAPIEvent: %v\n", event)
						continue
					}
					// We need to send an Acknowledge to the slack server
					socketClient.Ack(*event.Request)
					// Now we have an Events API event, but this event type can in turn be many types, so we actually need another type switch
					err := handleEventMessage(eventsAPIEvent, client)
					if err != nil {
						// Replace with actual err handeling
						log.Fatal(err)
					}
				// Handle Slash Commands
				case socketmode.EventTypeSlashCommand:
					// Just like before, type cast to the correct event type, this time a SlashEvent
					command, ok := event.Data.(slack.SlashCommand)
					if !ok {
						log.Printf("Could not type cast the message to a SlashCommand: %v\n", command)
						continue
					}

					// handleSlashCommand will take care of the command
					payload, err := handleSlashCommand(command, client)
					if err != nil {
						log.Fatal(err)
					}
					// Dont forget to acknowledge the request and send the payload
					socketClient.Ack(*event.Request, payload)
				case socketmode.EventTypeInteractive:

					interaction, ok := event.Data.(slack.InteractionCallback)
					if !ok {
						log.Printf("Could not type cast the message to a Interaction callback: %v\n", interaction)
						continue
					}

					err := handleInteractionEvent(interaction, client)
					if err != nil {
						log.Fatal(err)
					}
					socketClient.Ack(*event.Request)

				}

			}

		}
	}(ctx, client, socketClient)

	socketClient.Run()
}

func handleInteractionEvent(interaction slack.InteractionCallback, client *slack.Client) error {
	// This is where we would handle the interaction
	// Switch depending on the Type
	log.Printf("The action called is: %s\n", interaction.ActionID)
	log.Printf("The response was of type: %s\n", interaction.Type)
	switch interaction.Type {
	case slack.InteractionTypeBlockActions:
		// This is a block action, so we need to handle it

		for _, action := range interaction.ActionCallback.BlockActions {
			log.Printf("%+v", action)
			log.Println("Selected option: ", action.SelectedOptions)

		}

	default:

	}

	return nil
}

// handleSlashCommand will take a slash command and route to the appropriate function
func handleSlashCommand(command slack.SlashCommand, client *slack.Client) (interface{}, error) {
	// We need to switch depending on the command
	switch command.Command {
	case "/hello":
		// This was a hello command, so pass it along to the proper function
		return nil, handleHelloCommand(command, client)
	case "/was-this-article-useful":
		return handleIsArticleGood(command, client)
	}

	return nil, nil
}

// handleIsArticleGood will trigger a Yes or No question to the initializer
func handleIsArticleGood(command slack.SlashCommand, client *slack.Client) (interface{}, error) {
	// Create the attachment and assigned based on the message
	attachment := slack.Attachment{}

	// Create the checkbox element
	checkbox := slack.NewCheckboxGroupsBlockElement("answer",
		slack.NewOptionBlockObject("yes", &slack.TextBlockObject{Text: "Yes", Type: slack.MarkdownType}, &slack.TextBlockObject{Text: "Did you Enjoy it?", Type: slack.MarkdownType}),
		slack.NewOptionBlockObject("no", &slack.TextBlockObject{Text: "No", Type: slack.MarkdownType}, &slack.TextBlockObject{Text: "Did you Dislike it?", Type: slack.MarkdownType}),
	)
	// Create the Accessory that will be included in the Block and add the checkbox to it
	accessory := slack.NewAccessory(checkbox)
	// Add Blocks to the attachment
	attachment.Blocks = slack.Blocks{
		BlockSet: []slack.Block{
			// Create a new section block element and add some text and the accessory to it
			slack.NewSectionBlock(
				&slack.TextBlockObject{
					Type: slack.MarkdownType,
					Text: "Did you think this article was helpful?",
				},
				nil,
				accessory,
			),
		},
	}

	attachment.Text = "Rate the tutorial"
	attachment.Color = "#4af030"
	return attachment, nil
}

// handleHelloCommand will take care of /hello submissions
func handleHelloCommand(command slack.SlashCommand, client *slack.Client) error {
	// The Input is found in the text field so
	// Create the attachment and assigned based on the message
	attachment := slack.Attachment{}
	// Add Some default context like user who mentioned the bot
	attachment.Fields = []slack.AttachmentField{
		{
			Title: "Date",
			Value: time.Now().String(),
		}, {
			Title: "Initializer",
			Value: command.UserName,
		},
	}

	// Greet the user
	attachment.Text = fmt.Sprintf("Hello %s", command.Text)
	attachment.Color = "#4af030"

	// Send the message to the channel
	// The Channel is available in the command.ChannelID
	_, _, err := client.PostMessage(command.ChannelID, slack.MsgOptionAttachments(attachment))
	if err != nil {
		return fmt.Errorf("failed to post message: %w", err)
	}
	return nil
}

// handleEventMessage will take an event and handle it properly based on the type of event
func handleEventMessage(event slackevents.EventsAPIEvent, client *slack.Client) error {
	switch event.Type {
	// First we check if this is an CallbackEvent
	case slackevents.CallbackEvent:

		innerEvent := event.InnerEvent
		// Yet Another Type switch on the actual Data to see if its an AppMentionEvent
		switch ev := innerEvent.Data.(type) {
		case *slackevents.AppMentionEvent:
			// The application has been mentioned since this Event is a Mention event
			err := handleAppMentionEvent(ev, client)
			if err != nil {
				return err
			}
		}
	default:
		return errors.New("unsupported event type")
	}
	return nil
}

// handleAppMentionEvent is used to take care of the AppMentionEvent when the bot is mentioned
func handleAppMentionEvent(event *slackevents.AppMentionEvent, client *slack.Client) error {

	// Grab the user name based on the ID of the one who mentioned the bot
	user, err := client.GetUserInfo(event.User)
	if err != nil {
		return err
	}
	// Check if the user said Hello to the bot
	text := strings.ToLower(event.Text)

	// Create the attachment and assigned based on the message
	attachment := slack.Attachment{}
	// Add Some default context like user who mentioned the bot
	attachment.Fields = []slack.AttachmentField{
		{
			Title: "Date",
			Value: time.Now().String(),
		}, {
			Title: "Initializer",
			Value: user.Name,
		},
	}
	if strings.Contains(text, "hello") {
		// Greet the user
		attachment.Text = fmt.Sprintf("Hello %s", user.Name)
		attachment.Pretext = "Greetings"
		attachment.Color = "#4af030"
	} else {
		// Send a message to the user
		attachment.Text = fmt.Sprintf("How can I help you %s?", user.Name)
		attachment.Pretext = "How can I be of service"
		attachment.Color = "#3d3d3d"
	}
	// Send the message to the channel
	// The Channel is available in the event message
	_, _, err = client.PostMessage(event.Channel, slack.MsgOptionAttachments(attachment))
	if err != nil {
		return fmt.Errorf("failed to post message: %w", err)
	}
	return nil
}

func sendmessage(channelID string, client *slack.Client) {
	// Create the Slack attachment that we will send to the channel
	attachment := slack.Attachment{
		Pretext: "Super Bot Message",
		Text:    "some text",
		// Color Styles the Text, making it possible to have like Warnings etc.
		Color: "#36a64f",
		// Fields are Optional extra data!
		Fields: []slack.AttachmentField{
			{
				Title: "Date",
				Value: time.Now().String(),
			},
		},
	}
	// PostMessage will send the message away.
	// First parameter is just the channelID, makes no sense to accept it
	_, timestamp, err := client.PostMessage(
		channelID,
		// uncomment the item below to add a extra Header to the message, try it out :)
		//slack.MsgOptionText("New message from bot", false),
		slack.MsgOptionAttachments(attachment),
	)

	if err != nil {
		panic(err)
	}

	fmt.Printf("Message sent at %s", timestamp)
}
