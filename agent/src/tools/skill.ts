import { AgentSkill } from '../types'
import { tool } from 'ai'
import { z } from 'zod'

interface SkillToolParams {
  skills: AgentSkill[]
  useSkill: (skill: AgentSkill, reason: string) => void
}

export const getSkillTools = ({ skills, useSkill }: SkillToolParams) => {
  const useSkillTool = tool({
    description: 'Use a skill if you think it is relevant to the current task',
    inputSchema: z.object({
      skillName: z.string().describe('The name of the skill to use'),
      reason: z.string().describe('The reason why you think this skill is relevant to the current task'),
    }),
    execute: async ({ skillName, reason }) => {
      const skill = skills.find((s) => s.name === skillName)
      if (!skill) {
        return { error: 'Skill not found' }
      }
      await useSkill(skill, reason)
      return {
        success: true,
        skillName,
        reason,
      }
    },
  })

  return {
    'use_skill': useSkillTool,
  }
}