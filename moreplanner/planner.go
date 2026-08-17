package moreplanner

import (
	"fmt"

	"github.com/adrianpriza-ai/alps/config"
	"github.com/adrianpriza-ai/alps/more"
)

// OperationType represents the type of operation being performed
type OperationType string

const (
	OperationInstall OperationType = "install"
	OperationRemove  OperationType = "remove"
	OperationUpgrade OperationType = "upgrade"
	OperationPurge   OperationType = "purge"
)

// Plan represents a validated execution plan
type Plan struct {
	Entry          *more.Entry
	Operation      OperationType
	Commands       []Command
	NeedsPrivilege bool
	NeedsDownload  bool
	Validated      bool
}

// Command represents a single command in the execution plan
type Command struct {
	Type       CommandType
	Program    string
	Args       []string
	Env        []string
	WorkingDir string
	IsShell    bool
	IsDownload bool
	Digest     string // SHA-256 digest for downloads
	SourceLine string // Original source line for debugging
	Stage      ExecutionStage
}

// CommandType represents the type of command
type CommandType string

const (
	CommandTypeBuildEnv CommandType = "build_env"
	CommandTypeAfterEnv CommandType = "after_env"
	CommandTypeInstall  CommandType = "install"
	CommandTypeRemove   CommandType = "remove"
	CommandTypeUpgrade  CommandType = "upgrade"
	CommandTypePurge    CommandType = "purge"
	CommandTypeDownload CommandType = "download"
	CommandTypeScript   CommandType = "script"
)

// ExecutionStage represents when a command should be executed
type ExecutionStage string

const (
	StagePreInstall  ExecutionStage = "pre_install"
	StageInstall     ExecutionStage = "install"
	StagePostInstall ExecutionStage = "post_install"
	StagePreRemove   ExecutionStage = "pre_remove"
	StageRemove      ExecutionStage = "remove"
	StagePostRemove  ExecutionStage = "post_remove"
)

// Planner creates execution plans from manifest entries
type Planner struct {
	cfg *config.Config
}

// NewPlanner creates a new planner
func NewPlanner(cfg *config.Config) *Planner {
	return &Planner{
		cfg: cfg,
	}
}

// CreatePlan creates an execution plan for an entry and operation
func (p *Planner) CreatePlan(entry *more.Entry, op OperationType) (*Plan, error) {
	if entry == nil {
		return nil, fmt.Errorf("entry cannot be nil")
	}

	// Validate the entry first
	if err := p.validateEntry(entry); err != nil {
		return nil, fmt.Errorf("entry validation failed: %w", err)
	}

	// Scrape commands based on operation type
	lines, err := p.scrapeCommands(entry, op)
	if err != nil {
		return nil, fmt.Errorf("command scraping failed: %w", err)
	}

	// Parse commands into the plan
	commands, err := p.parseCommands(lines, op)
	if err != nil {
		return nil, fmt.Errorf("command parsing failed: %w", err)
	}

	// Determine if plan needs privilege
	needsPrivilege := p.determinePrivilegeNeeds(commands)

	// Determine if plan needs downloads
	needsDownload := p.determineDownloadNeeds(commands)

	return &Plan{
		Entry:          entry,
		Operation:      op,
		Commands:       commands,
		NeedsPrivilege: needsPrivilege,
		NeedsDownload:  needsDownload,
		Validated:      true,
	}, nil
}

// validateEntry validates an entry without executing anything
func (p *Planner) validateEntry(entry *more.Entry) error {
	// Delegate to existing validation logic but ensure no execution
	return more.Validate(entry)
}

// scrapeCommands extracts command blocks from an Entry based on the operation type
func (p *Planner) scrapeCommands(entry *more.Entry, op OperationType) ([]string, error) {
	// Delegate to existing scraping logic
	var lines []string

	switch op {
	case OperationInstall:
		if len(entry.CmdLines) == 0 {
			return nil, fmt.Errorf("package %q has no install commands (cmd_begin/cmd_end)", entry.Name)
		}
		lines = append([]string(nil), entry.CmdLines...)
	case OperationRemove:
		if len(entry.RemoveLines) > 0 {
			lines = append([]string(nil), entry.RemoveLines...)
		} else {
			return nil, fmt.Errorf("package %q has no remove commands (remove_begin/remove_end)", entry.Name)
		}
	case OperationUpgrade:
		if len(entry.UpgradeLines) > 0 {
			lines = append([]string(nil), entry.UpgradeLines...)
		} else {
			// Fall back to install commands for upgrade
			if len(entry.CmdLines) > 0 {
				lines = append([]string(nil), entry.CmdLines...)
			} else {
				return nil, fmt.Errorf("package %q has no upgrade commands (upgrade_begin/upgrade_end or cmd_begin/cmd_end)", entry.Name)
			}
		}
	case OperationPurge:
		if len(entry.PurgeLines) > 0 {
			lines = append([]string(nil), entry.PurgeLines...)
		} else {
			// Fall back to remove commands for purge
			if len(entry.RemoveLines) > 0 {
				lines = append([]string(nil), entry.RemoveLines...)
			} else {
				return nil, fmt.Errorf("package %q has no purge commands (purge_begin/purge_end or remove_begin/remove_end)", entry.Name)
			}
		}
	default:
		return nil, fmt.Errorf("unknown operation type: %s", op)
	}

	return lines, nil
}

// parseCommands parses command lines into structured commands
func (p *Planner) parseCommands(lines []string, op OperationType) ([]Command, error) {
	commands := make([]Command, 0, len(lines))

	for i, line := range lines {
		cmd, err := p.parseSingleCommand(line, op)
		if err != nil {
			return nil, fmt.Errorf("failed to parse command at line %d: %w", i+1, err)
		}
		commands = append(commands, cmd)
	}

	return commands, nil
}

// parseSingleCommand parses a single command line
func (p *Planner) parseSingleCommand(line string, op OperationType) (Command, error) {
	// This is a simplified parser - the full implementation would
	// handle macros, variable expansion, etc.
	// For now, we create a basic shell command.

	stage := p.determineStage(op, line)
	cmdType := p.determineCommandType(op, line)

	return Command{
		Type:       cmdType,
		Program:    "sh",
		Args:       []string{"-c", line},
		IsShell:    true,
		SourceLine: line,
		Stage:      stage,
	}, nil
}

// determineStage determines the execution stage for a command
func (p *Planner) determineStage(op OperationType, line string) ExecutionStage {
	// Simplified logic - full implementation would analyze the command
	switch op {
	case OperationInstall:
		return StageInstall
	case OperationRemove:
		return StageRemove
	case OperationUpgrade:
		return StageInstall
	case OperationPurge:
		return StageRemove
	default:
		return StageInstall
	}
}

// determineCommandType determines the command type
func (p *Planner) determineCommandType(op OperationType, line string) CommandType {
	switch op {
	case OperationInstall:
		return CommandTypeInstall
	case OperationRemove:
		return CommandTypeRemove
	case OperationUpgrade:
		return CommandTypeUpgrade
	case OperationPurge:
		return CommandTypePurge
	default:
		return CommandTypeInstall
	}
}

// determinePrivilegeNeeds determines if the plan requires privilege escalation
func (p *Planner) determinePrivilegeNeeds(commands []Command) bool {
	// Simplified logic - full implementation would analyze commands
	// For now, assume install/upgrade/remove/purge need privilege
	for _, cmd := range commands {
		switch cmd.Type {
		case CommandTypeInstall, CommandTypeUpgrade, CommandTypeRemove, CommandTypePurge:
			return true
		}
	}
	return false
}

// determineDownloadNeeds determines if the plan requires downloads
func (p *Planner) determineDownloadNeeds(commands []Command) bool {
	// Check if any commands are downloads
	for _, cmd := range commands {
		if cmd.IsDownload {
			return true
		}
	}
	return false
}
