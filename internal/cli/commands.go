package cli

// CommandHandler is a function that handles a command.
type CommandHandler func(args []string) (handled bool, exit bool)

// CommandRegistry manages available commands.
type CommandRegistry struct {
	commands map[string]CommandHandler
}

// NewCommandRegistry creates a new command registry with default commands.
func NewCommandRegistry() *CommandRegistry {
	r := &CommandRegistry{
		commands: make(map[string]CommandHandler),
	}
	return r
}

// Register adds a new command.
func (r *CommandRegistry) Register(name string, handler CommandHandler) {
	r.commands[name] = handler
}

// Get retrieves a command handler.
func (r *CommandRegistry) Get(name string) (CommandHandler, bool) {
	h, ok := r.commands[name]
	return h, ok
}

// List returns all registered commands.
func (r *CommandRegistry) List() []string {
	names := make([]string, 0, len(r.commands))
	for name := range r.commands {
		names = append(names, name)
	}
	return names
}
