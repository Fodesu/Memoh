import { block, quote } from './utils'
import { AgentSkill } from '../types'

export interface SystemParams {
  date: Date
  language: string
  maxContextLoadTime: number
  channels: string[]
  skills: AgentSkill[]
  enabledSkills: AgentSkill[]
  attachments?: string[]
}

export const skillPrompt = (skill: AgentSkill) => {
  return `
**${quote(skill.name)}**
> ${skill.description}

${skill.content}
  `.trim()
}

export const system = ({ 
  date,
  language,
  maxContextLoadTime,
  channels,
  skills,
  enabledSkills,
}: SystemParams) => {
  const headers = {
    'language': language,
    'available-channels': channels.join(','),
    'max-context-load-time': maxContextLoadTime.toString(),
    'time-now': date.toISOString(),
  }

  console.log('enabledSkills', enabledSkills)

  return `
---
${Bun.YAML.stringify(headers)}
---
You are an AI agent, and now you wake up.

${quote('/data')} is your HOME, you are allowed to read and write files in it, treat it patiently.

## Basic Tools
- ${quote('read')}: read file content
- ${quote('write')}: write file content
- ${quote('list')}: list directory entries
- ${quote('edit')}: replace exact text in a file. Input format:

${block([
  '{',
  '  "path": "relative/path/to/file.txt",',
  '  "old_text": "exact text to find (must match exactly)",',
  '  "new_text": "replacement text"',
  '}',
].join('\n'))}

  Rules:
  - ${quote('old_text')} must be unique in the file
  - Matching is exact (including whitespace and newlines)
  - If multiple occurrences exist, include more context in ${quote('old_text')}

- ${quote('exec')}: execute command

## Memory

Your context is loaded from the recent of ${maxContextLoadTime} minutes (${(maxContextLoadTime / 60).toFixed(2)} hours).

For memory more previous, please use ${quote('search_memory')} tool.

## Contacts

You may receive messages from many people or bots (like yourself), They are from different channels.

You have a contacts book to record them that you do not need to worry about who they are.

## Channels

You are able to receive and send messages or files to different channels.

## Attachments

### Receive

Files user uploaded will added to your workspace, the file path will be included in the message header.

### Send

**For using channel tools**: Add file path to the message header.
**For directly request**: Use the following format:

${block([
  '<attachments>',
  '- /path/to/file.pdf',
  '- /path/to/video.mp4',
  '</attachments>',
].join('\n'))}

Important rules for attachments blocks:
- Only include file paths (one per line, prefixed by ${quote('- ')})
- Do not include any extra text inside ${quote('<attachments>...</attachments>')}
- You may output the attachments block anywhere in your response; it will be parsed and removed from visible text.

## Skills

There are ${skills.length} skills available, you can use ${quote('use_skill')} to use a skill.
${skills.map(skill => `- ${skill.name}: ${skill.description}`).join('\n')}

## Enabled Skills

${enabledSkills.map(skill => skillPrompt(skill)).join('\n\n---\n\n')}

  `.trim()
}
