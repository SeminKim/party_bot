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
	Name                   string
	Participants           map[*discordgo.User]NIL
	ParticipantsID         map[string]NIL
	Owner                  *discordgo.User
	TargetPopulation       int
	CurrentPopulation      int
	Origin                 *discordgo.Interaction
	OriginMessageReference discordgo.MessageReference
	OriginMessageID        string // shortcut for OriginMessageReference.MessageID
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
	ret := fmt.Sprintf("%s님의 %s:(%d/%d)", p.Owner.Username, p.Name, p.CurrentPopulation, p.TargetPopulation)
	ret += "\n참가자: "
	for user, _ := range p.Participants {
		ret += user.Username + " "
	}
	return ret
}

// remove party from active-party list.
func (p *Party) removeParty() {
	delete(ActiveParties, p.Owner.ID)
}

// Add a person to party. This also increments current population field on party.
func (p *Party) addRegistrant(registrant *discordgo.User) {
	p.Participants[registrant] = NIL{}
	p.ParticipantsID[registrant.ID] = NIL{}
	p.CurrentPopulation += 1
}

// Remove a person from party. This also decrements current population field on party.
func (p *Party) removeRegistrant(registrant *discordgo.User) {
	// to ensure deletion, check ID.
	var target *discordgo.User
	target = nil
	for foo, _ := range p.Participants {
		if foo.ID == registrant.ID {
			target = foo
		}
	}
	if target == nil {
		log.Println("Fail while removing registrant: " + registrant.Username)
	}
	delete(p.Participants, target)
	delete(p.ParticipantsID, registrant.ID)
	p.CurrentPopulation -= 1
}

// helper function for responding with text.
// if delete_after is positive integer, the response will be deleted after that seconds.
func respondWithSimpleContent(s *discordgo.Session, i *discordgo.InteractionCreate, my_msg string, delete_after int) {
	err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: my_msg,
		},
	})
	if err != nil {
		log.Println("Error while responding with sipmle message: " + my_msg)
	}
	if delete_after > 0 {
		time.AfterFunc(time.Second*time.Duration(delete_after), func() {
			s.InteractionResponseDelete(i.Interaction)
		})
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
		log.Println("delete: " + curr.Name)
		s.ApplicationCommandDelete(*AppID, *GuildID, curr.ID)
	}
}

func deleteAllParties() {
	for _, p := range ActiveParties {
		s.ChannelMessageDelete(p.Origin.ChannelID, p.OriginMessageID)
		log.Println(p.Name + " by " + p.Owner.Username + " deleted.")
		p.removeParty()
	}
}

var (
	commandsHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"겜팟구": openParty,
		"롤할롤": openLOL,
		"끌올":  remindParty,
	}
	componenetHandlers = map[string]func(s *discordgo.Session, i *discordgo.InteractionCreate){
		"get_in_party":  getInParty,
		"get_out_party": getOutParty,
		"cancel_party":  cancelParty,
	}
)

// Origin message ID should be filled later, since it stores the bot's respond(which is not sent yet).
// It does not check validity of parsed value. Furthermore, it does not add the party to ActiveParties.
// NOTE: This does not fill OriginMessageID and OriginMessageReference!!
func initializeParty(i *discordgo.InteractionCreate, name string, target int) Party {
	party := Party{
		Name:              name,
		Participants:      make(map[*discordgo.User]NIL),
		ParticipantsID:    make(map[string]NIL),
		Owner:             i.Member.User,
		TargetPopulation:  target,
		CurrentPopulation: 0,
		Origin:            i.Interaction,
	}
	party.addRegistrant(i.Member.User)
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
		respondWithSimpleContent(s, i, "이미 모집중인 팟이 있습니다.", -1)
		s.ChannelMessageSendReply(i.ChannelID, "새 파티를 구하려면 먼저 닫아주세요.", &found.OriginMessageReference)
		return
	}
	party := parseAndInitializeParty(i)
	// Invalid option.
	if party.TargetPopulation < 2 {
		respondWithSimpleContent(s, i, "인원수는 2 이상의 자연수여야 합니다.", -1)
		return
	}
	// Normal usecase.
	ActiveParties[i.Member.User.ID] = &party
	err := respondWithPartyButtonsMessage(s, i, &party)
	if err != nil {
		log.Println("Error while responding open_party with buttons: ")
	}
	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Println("Error while getting sended response")
	}
	party.OriginMessageID = msg.ID
	party.OriginMessageReference = *msg.Reference()
	log.Println("openParty called by ", i.Member.User.Username, ", message ID is ", msg.ID)
}

// Open party with name "롤할롤" and target population 5.
func openLOL(s *discordgo.Session, i *discordgo.InteractionCreate) {
	found, ok := findPartyByAuthor(i.Interaction.Member)
	// Cannot make more than one party.
	if ok {
		respondWithSimpleContent(s, i, "이미 모집중인 팟이 있습니다.", -1)
		s.ChannelMessageSendReply(i.ChannelID, "새 파티를 구하려면 먼저 닫아주세요.", &found.OriginMessageReference)
		return
	}
	// Normal usecase.
	party := initializeParty(i, "롤할롤", 5)
	ActiveParties[i.Member.User.ID] = &party
	err := respondWithPartyButtonsMessage(s, i, &party)
	if err != nil {
		log.Println("Error while responding open_party with buttons.")
	}
	msg, err := s.InteractionResponse(i.Interaction)
	if err != nil {
		log.Println("Error while getting sended response")
	}
	party.OriginMessageID = msg.ID
	party.OriginMessageReference = *msg.Reference()
	log.Println("openLOL called by ", i.Member.User.Username, ", message ID is ", msg.ID)
}

// Remind previous opened party to channel.
func remindParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	found, ok := findPartyByAuthor(i.Interaction.Member)
	// Not a owner of party.
	if !ok {
		respondWithSimpleContent(s, i, "활성화된 파티의 주인이 아닙니다.", 3)
		return
	}
	respondWithSimpleContent(s, i, "끌올중...", -1)
	s.InteractionResponseDelete(i.Interaction) // Is there better way to do it?
	s.ChannelMessageSendReply(i.ChannelID, "ㄹㅇ루 너만오면 ㄱ", &found.OriginMessageReference)
}

// Register member to the party.
func getInParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	registrant := i.Member.User
	found, ok := findPartyByMessageID(i.Message.ID)
	// try to get in party, but no party found (hope this would not happen.)
	if !ok {
		log.Println("Error - not found: " + registrant.ID)
		printAllParties()
		respondWithSimpleContent(s, i, "Sorry, unexpected error happened.", -1)
		return
	}
	_, ok = found.ParticipantsID[registrant.ID]
	// if already registered, deny it.
	if ok {
		respondWithSimpleContent(s, i, "이미 등록된 참가자입니다.", 3)
		return
	}
	// normal usecase.
	found.addRegistrant(registrant)
	s.ChannelMessageEdit(i.ChannelID, found.OriginMessageID, found.pretty_print()) // update message.
	respondWithSimpleContent(s, i, "등록되었습니다.", 3)
	// when target population is achieved.
	if found.CurrentPopulation == found.TargetPopulation {
		foo := found.Name + "ㄱㄱ: "
		for user, _ := range found.Participants {
			foo += user.Mention() + " "
		}
		s.ChannelMessageSend(i.ChannelID, foo)
		// clean up messages.
		s.ChannelMessageDelete(i.ChannelID, found.OriginMessageID)
		// delete from active party list.
		found.removeParty()
	}
}

// Unregister member to the party.
func getOutParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	registrant := i.Member.User
	found, ok := findPartyByMessageID(i.Message.ID)
	// try to get out party, but no party found (hope this would not happen.)
	if !ok {
		log.Println("Error - not found: " + registrant.ID)
		printAllParties()
		respondWithSimpleContent(s, i, "Sorry, unexpected error happened.", -1)
		return
	}
	_, ok = found.ParticipantsID[registrant.ID]
	// if not registered, deny it.
	if !ok {
		respondWithSimpleContent(s, i, "등록되지 않은 참가자입니다.", 3)
		return
	}
	// if user is owner, deny it.
	if found.Owner.ID == registrant.ID {
		respondWithSimpleContent(s, i, "파티장은 빠꾸칠 수 없습니다.", 3)
		return
	}

	// normal usecase.
	found.removeRegistrant(registrant)
	s.ChannelMessageEdit(i.ChannelID, found.OriginMessageID, found.pretty_print()) // update message.
	respondWithSimpleContent(s, i, "등록 취소되었습니다.", 3)
}

// Owner only, close the party.
func cancelParty(s *discordgo.Session, i *discordgo.InteractionCreate) {
	found, ok := findPartyByAuthor(i.Member)
	if ok {
		found.removeParty()
		s.ChannelMessageDelete(found.Origin.ChannelID, found.OriginMessageID) // interaction delete or message delete?
		// s.InteractionResponseDelete(*AppID, found.Origin)
		respondWithSimpleContent(s, i, "정상적으로 종료되었습니다.", 3)
	} else {
		respondWithSimpleContent(s, i, "활성화된 파티의 주인이 아닙니다.", 3)
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
	_, err = s.ApplicationCommandCreate(*AppID, *GuildID, &discordgo.ApplicationCommand{
		Name:        "롤할롤",
		Description: "너만오면 5인큐임을 알리세요",
	})
	if err != nil {
		log.Fatalf("Cannot create slash command: %v", err)
	}
	_, err = s.ApplicationCommandCreate(*AppID, *GuildID, &discordgo.ApplicationCommand{
		Name:        "끌올",
		Description: "진짜로 너만오면 됨을 알리세요",
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
	defer deleteAllParties()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)
	<-stop
	log.Println("Gracefully shutdowning")
}
