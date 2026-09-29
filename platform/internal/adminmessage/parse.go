package adminmessage

// Command retains literal audience expressions until the snapshot is created.
// Missing content requests an explicit attachment; it never captures free text.
type Command struct {
	Recipients []string `json:"recipients"`
	Content    Content  `json:"content"`
	Template   bool     `json:"template"`
	Forward    bool     `json:"forward"`
}

func (c Command) NeedsInput() bool { return c.Forward || c.Content.Text == "" }

func ParseCommand(command string) (Command, error) {
	args, err := commandWords(command)
	if err != nil {
		return Command{}, err
	}
	return ParseArguments(args)
}

// ParseArguments also accepts arguments whose Telegram CODE/PRE boundaries were
// already decoded by the transport adapter.
func ParseArguments(args []string) (Command, error) {
	var result Command
	if len(args) == 0 || args[0] != "/send_message_to" {
		return result, invalid()
	}
	modeSeen := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--template":
			result.Template = true
		case "--forward":
			if modeSeen {
				return Command{}, invalid()
			}
			modeSeen = true
			result.Forward = true
		case "--msg", "--html", "--md":
			if modeSeen || i+1 >= len(args) {
				return Command{}, invalid()
			}
			modeSeen = true
			result.Content.ParseMode = map[string]string{"--html": parseHTML, "--md": "Markdown"}[args[i]]
			i++
			result.Content.Text = args[i]
		default:
			if len(args[i]) >= 2 && args[i][:2] == "--" {
				return Command{}, invalid()
			}
			result.Recipients = append(result.Recipients, args[i])
		}
	}
	if len(result.Recipients) == 0 || (result.Template && result.Forward) {
		return Command{}, invalid()
	}
	return result, nil
}
