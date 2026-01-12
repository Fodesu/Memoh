import { Schedule } from '@memoh/shared'
import { time } from './shared'

export interface ScheduleParams {
  schedule: Schedule
  locale?: Intl.LocalesArgument
  date: Date
}

export const schedule = (params: ScheduleParams) => {
  return `
---
notice: **This is a scheduled task automatically send to you by the system, not the user input**
${time({ date: params.date, locale: params.locale })}
schedule-name: ${params.schedule.name}
schedule-description: ${params.schedule.description}
schedule-id: ${params.schedule.id}
max-calls: ${params.schedule.maxCalls ?? 'Unlimited'}
cron-pattern: ${params.schedule.pattern}
---

**COMMAND**

${params.schedule.command}
  `.trim()
}