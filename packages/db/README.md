# @memoh/db

Database package for memoh project using Drizzle ORM and PostgreSQL.

## Database Schema

### Tables

- **model**: AI model configurations
- **history**: Chat history records
- **settings**: User settings and preferences
- **users**: User accounts with authentication

See [USERS_SCHEMA.md](./USERS_SCHEMA.md) for detailed user table documentation.

## Quick Start

### Initialize Database

```bash
pnpm push
```

### Generate Schema

```bash
pnpm generate
```

### Start Studio

```bash
pnpm studio
```