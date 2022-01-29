package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	// "strings"
	"github.com/bwmarrin/discordgo"
)

// Small trick to use map like set, not using further memory.
type NIL struct{}

// Struct for managing party.
type Party struct {
	Name              string
	Participants      map[*discordgo.User]NIL
	ParticipantsID    map[string]NIL
	Owner             *discordgo.User
	TargetPopulation  int
	CurrentPopulation int
	Origin            *discordgo.Interaction
	OriginMessageID   string
}

// Map containing all active parties.
var ActiveParties map[string]*Party

// Debug-purpose
func printAllParties() {
	for _, v := range ActiveParties {
		log.Println(v.pretty_print())
	}
}

// maybe not needed anymore?
func findPartyByAuthor(target *discordgo.Member) (ret *Party, ok bool) {
	ret, ok = ActiveParties[target.User.ID]
	return
}

func findPartyByMessageID(id string) (ret *Party, ok bool) {
	for _, v := range ActiveParties {
		if v.OriginMessageID == id {
			ret, ok = v, true
			return
		}
	}
	ret, ok = nil, false
	return
}

// Return pretty-print string for stuct Party.
func (p *Party) pretty_print() string {
	return fmt.Sprintf("%s님의 %s:(%d/%d)", p.Owner.Username, p.Name, p.CurrentPopulation, p.TargetPopulation)
}

// remove party from active-party list.
func (p *Party) removeParty() {
	delete(ActiveParties, p.Owner.ID)
}

func respondWithSimpleContent(s *discordgo.Session, i *discordgo.InteractionCreate, my_msg string) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: my_msg,
		},
	})
	if err != nil {
		log.Println("Error while responding with sipmle message: " + my_msg)
	}
}

// helper function for openParty.
// respond with messages... THAT core message with some buttons.
func respondWithPartyButtonsMessage(s *discordgo.Session, i *discordgo.InteractionCreate, p *Party) (err error) {
	err = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: p.pretty_print(),
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.Button{
							Label:    "드가자~",
							Style:    discordgo.SuccessButton,
							Disabled: false,
							CustomID: "get_in_party",
						},
						discordgo.Button{
							Label:    "빠꾸",
							Style:    discordgo.DangerButton,
							Disabled: false,
							CustomID: "get_out_party",
						},
						discordgo.Button{
							Label:    "폭파",
							Style:    discordgo.SecondaryButton,
							Disabled: false,
							CustomID: "cancel_party",
						},
					},
				},
			},
		},
	})
	return
}

// Bot parameters
var (
	GuildID        = flag.String("guild", "", "Test guild ID. If not passed - bot registers commands globally")
	BotToken       = flag.String("token", "", "Bot access token")
	AppID          = flag.String("app", "", "App ID")
	RemoveCommands = flag.Bool("rmcmd", true, "Remove all commands after shutdowning or not")
)

var s *discordgo.Session

func init() { flag.Parse() }

func init() {
	var err error
	s, err = discordgo.New("Bot " + *BotToken)
	if err != nil {
		log.Fatalf("Invalid bot parameters: %v", err)
	}
	ActiveParties = map[string]*Party{}
}

// Delete all registered commands.
func deleteAllCommands() {
	commands, _ := s.ApplicationCommands(*AppID, *GuildID)
	for _, curr := range commands {
		fmt.Println("delete: " + curr.Name)
		s.ApplicationCommandDelete(*AppID, *GuildID, curr.ID)
	}
}

var (
	commandsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"겜팟구": openParty,
		"롤할롤": open_lol,
	}
	componenetHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"get_in_party":  getInParty,
		"get_out_party": getOutParty,
		"cancel_party":  cancelParty,
	}
)

// Origin message ID should be filled later, since it stores the bot's respond(which is not sent yet).
// It does not check validity of parsed value. Furthermore, it does not add the party to ActiveParties.
func initializeParty(i *discordgo.InteractionCreate, name string, target int) Party {
	party := Party{
		Name:              name,
		Participants:      make(map[*discordgo.User]NIL),
		ParticipantsID:    make(map[string]NIL),
		Owner:             i.Member.User,
		TargetPopulation:  target,
		CurrentPopulation: 1,
		Origin:            i.Interaction,
		OriginMessageID:   "", // should be filled later
	}
	party.Participants[i.Member.User] = NIL{}
	party.ParticipantsID[i.Member.User.ID] = NIL{}
	return party
}

// parse data from interaction and call initializeParty.
func parseAndInitializeParty(i *discordgo.InteractionCreate) Party {
	name := i.ApplicationCommandData().Options[0].StringValue()
	target := int(i.ApplicationCommandData().Options[1].IntValue())
	return initializeParty(i, name, target)
}

// Parse Interaction and start new "Party".
func openParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	found, ok := findPartyByAuthor(i.Interaction.Member)
	// Cannot make more than one party.
	if ok {
		respondWithSimpleContent(s, i, "이미 모집중인 팟이 있습니다.")
		prevResponse, _ := s.InteractionResponse(*AppID, found.Origin)
		s.ChannelMessageSendReply(i.ChannelID, "새 파티를 구하려면 먼저 닫아주세요.", prevResponse.Reference())
		return
	}
	party := parseAndInitializeParty(i)
	// Invalid option.
	if party.TargetPopulation < 2 {
		respondWithSimpleContent(s, i, "인원수는 2 이상의 자연수여야 합니다.")
		return
	}
	// Normal usecase.
	ActiveParties[i.Member.User.ID] = &party
	err := respondWithPartyButtonsMessage(s, i, &party)
	if err != nil {
		log.Println("Error while responding open_party with buttons.")
	}
	msg, err := s.InteractionResponse(*AppID, i.Interaction)
	if err != nil {
		log.Println("Error while getting sended response")
	}
	party.OriginMessageID = msg.ID
}

// Open party with name "롤할롤" and target population 5.
func open_lol(s *discordgo.Session, i *discordgo.InteractionCreate) {}

// Register member to the party.
func getInParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	registrant := i.Member.User
	found, ok := findPartyByMessageID(i.Message.ID)
	if !ok {
		log.Println("Error: not found")
		respondWithSimpleContent(s, i, "Sorry, unexpected error happened.")
	} else {
		_, ok = found.ParticipantsID[registrant.ID]
		// if already registered, deny it.
		if ok {
			respondWithSimpleContent(s, i, "이미 등록된 참가자입니다.")
		} else {
			found.CurrentPopulation += 1
			// TODO: Update message.
			found.Participants[registrant] = NIL{}
			found.ParticipantsID[registrant.ID] = NIL{}
			log.Println(found.Participants)
			// TODO: if target population is achieved, close and mention participants.
		}

	}
	// printAllParties()
}

// Unregister member to the party.
func getOutParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	// TODO: IMPLEMENTATION
	log.Println("get_out called.")
}

// Owner only, close the party.
func cancelParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	found, ok := findPartyByAuthor(i.Member)
	if ok {
		found.removeParty()
		s.InteractionResponseDelete(*AppID, found.Origin)
		respondWithSimpleContent(s, i, "정상적으로 종료되었습니다.")
		time.AfterFunc(time.Second*5, func() {
			s.InteractionResponseDelete(*AppID, i.Interaction)
		})
	} else {
		respondWithSimpleContent(s, i, "활성화된 파티의 주인이 아닙니다.")
	}
}

func main() {
	// At Ready
	s.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		log.Println("Bot is up!")
	})
	// Add Handler for Slash command and Button.
	s.AddHandler(func(s *discordgo.Session, i *discordgo.InteractionCreate) {
		switch i.Type {
		case discordgo.InteractionApplicationCommand:
			if h, ok := commandsHandlers[i.ApplicationCommandData().Name]; ok {
				h(s, i)
			}
		case discordgo.InteractionMessageComponent:
			if h, ok := componenetHandlers[i.MessageComponentData().CustomID]; ok {
				h(s, i)
			}
		default:
			log.Println("Unknown Interaction")
		}
	})
	// Register Slash Commands
	_, err := s.ApplicationCommandCreate(*AppID, *GuildID, &discordgo.ApplicationCommand{
		Name:        "겜팟구",
		Description: "너만오면ㄱ임을 알리세요",
		Options: []*discordgo.ApplicationCommandOption{
			{
				Type:        discordgo.ApplicationCommandOptionString,
				Name:        "이름",
				Description: "모집하는 파티 이름",
				Required:    true,
			},
			{
				Type:        discordgo.ApplicationCommandOptionInteger,
				Name:        "인원수",
				Description: "모집하려는 숫자",
				Required:    true,
			},
		},
	})
	if err != nil {
		log.Fatalf("Cannot create slash command: %v", err)
	}
	// Start Session.
	err = s.Open()
	if err != nil {
		log.Fatalf("Cannot open the session: %v", err)
	}
	// Delete all registered commands at end.
	defer s.Close()
	defer deleteAllCommands()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Gracefully shutdowning")
}
