import { pgTable, timestamp, uuid, jsonb } from 'drizzle-orm/pg-core'
import { users } from './users'

export const history = pgTable(
  'history', 
  {
    id: uuid('id').primaryKey().defaultRandom(),
    messages: jsonb('messages').notNull(),
    timestamp: timestamp('timestamp').notNull(),
    user: uuid('user').notNull().references(() => users.id),
  }
)