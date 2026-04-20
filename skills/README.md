# Skills Directory

This directory stores user-defined skills for GoClaw.

## Usage

Skills are defined as markdown files following the superpowers format:

```
skills/
├── my-skill.md      # Custom skill definition
└── another-skill.md
```

Each skill file should have frontmatter with `name` and `description`.

## Configuration

Skills are enabled/disabled in `extensions_config.json`:

```json
{
  "skills": {
    "my-skill": {
      "enabled": true
    }
  }
}
```

**Note:** User-defined skill files (`.md`) are ignored by git.
