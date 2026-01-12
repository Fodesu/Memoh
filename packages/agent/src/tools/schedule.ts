import { Schedule } from '@memoh/shared'
import { tool } from 'ai'
import z from 'zod'

export interface GetScheduleToolParams {
  onGetSchedules: () => Promise<Schedule[]>
  onRemoveSchedule: (id: string) => Promise<void>
  onSchedule: (schedule: Schedule) => Promise<void>
}

export const getScheduleTools = ({ onGetSchedules, onRemoveSchedule, onSchedule }: GetScheduleToolParams) => {
  const getSchedulesTool = tool({
    description: 'Get the list of schedules',
    inputSchema: z.object(),
    execute: async () => {
      const schedules = await onGetSchedules()
      return {
        success: true,
        schedules,
      }
    },
  })

  const removeScheduleTool = tool({
    description: 'Remove a schedule',
    inputSchema: z.object({
      id: z.string().describe('The id of the schedule'),
    }),
    execute: async ({ id }) => {
      await onRemoveSchedule(id)
    },
  })

  const scheduleTool = tool({
    description: 'Schedule a command',
    inputSchema: z.object({
      pattern: z.string().describe('The pattern of the schedule with **Cron Syntax**'),
      command: z.string().describe('The natural language command to execute, will send to you when the schedule is triggered'),
      name: z.string().describe('The name of the schedule'),
      description: z.string().describe('The description of the schedule'),
      maxCalls: z.number().describe('The maximum number of calls to the schedule').optional(),
    }),
    execute: async ({ pattern, command, name, description, maxCalls }) => {
      await onSchedule({ pattern, command, name, description, maxCalls })
    },
  })

  return {
    'get-schedules': getSchedulesTool,
    'remove-schedule': removeScheduleTool,
    'schedule': scheduleTool,
  }
}