// The capability catalog. It mirrors internal/authz.Catalog, which is the
// authority: the server refuses any name it does not know, so a drift between
// the two shows up as a visible refusal rather than a silent grant.
//
// Operator capabilities are the non-delegable tier. They can defeat a privacy
// boundary (a full export, a password takeover), so only the installation owner
// may grant them and the UI says so instead of offering a checkbox that always
// fails.
export interface Capability {
  name: string;
  label: string;
  detail: string;
  operator?: boolean;
}

export interface CapabilityGroup {
  title: string;
  detail: string;
  capabilities: Capability[];
}

export const capabilityGroups: CapabilityGroup[] = [
  {
    title: 'Everyday',
    detail: 'What an ordinary member needs to take part.',
    capabilities: [
      { name: 'channel.read', label: 'Read channels', detail: 'Open public channels.' },
      { name: 'message.send', label: 'Send messages', detail: 'Post in channels they can read.' },
      { name: 'message.react', label: 'React to messages', detail: 'Add and remove emoji.' },
      { name: 'message.upload', label: 'Upload files', detail: 'Attach files to messages.' },
      {
        name: 'voice.connect',
        label: 'Join voice',
        detail: 'Enter a voice channel and listen.'
      },
      { name: 'voice.speak', label: 'Speak in voice', detail: 'Transmit audio.' },
      {
        name: 'voice.share_screen',
        label: 'Share a screen',
        detail: 'Publish a screen or window.'
      }
    ]
  },
  {
    title: 'Moderation',
    detail: 'Authority over other people’s messages and calls.',
    capabilities: [
      { name: 'message.pin', label: 'Pin messages', detail: 'Pin and unpin in any channel.' },
      {
        name: 'message.delete_any',
        label: 'Delete any message',
        detail: 'Remove a message they did not send.'
      },
      {
        name: 'voice.moderate',
        label: 'Moderate voice',
        detail: 'Mute, deafen and move others.'
      },
      {
        name: 'mention.everyone',
        label: 'Mention everyone',
        detail: 'Notify the whole community at once.'
      },
      {
        name: 'audit.read',
        label: 'Read the audit log',
        detail: 'See the recorded administrative actions.'
      },
      {
        name: 'report.review_private',
        label: 'Review private message reports',
        detail:
          'Read the single messages people have chosen to report from their own direct conversations, and record what you concluded. It does not open those conversations, their history or their files.'
      }
    ]
  },
  {
    title: 'Management',
    detail: 'Authority over accounts and the shape of the community.',
    capabilities: [
      {
        name: 'member.manage',
        label: 'Manage accounts',
        detail: 'Create, ban and delete accounts.'
      },
      {
        name: 'channel.manage',
        label: 'Manage channels',
        detail: 'Create channels and change who can read them.'
      },
      {
        name: 'category.manage',
        label: 'Manage categories',
        detail: 'Create and reorder categories.'
      },
      { name: 'role.manage', label: 'Manage roles', detail: 'Create roles and assign them.' },
      {
        name: 'invite.manage',
        label: 'Manage invitations',
        detail: 'Issue and revoke invitation links.'
      },
      {
        name: 'settings.manage',
        label: 'Manage server settings',
        detail: 'Change workspace settings, health and backups.'
      }
    ]
  },
  {
    title: 'Operator',
    detail: 'Owner-only powers that can defeat a privacy boundary.',
    capabilities: [
      {
        name: 'operator.backup',
        label: 'Archive the server',
        detail: 'Download a full copy of every conversation.',
        operator: true
      },
      {
        name: 'operator.credential_recovery',
        label: 'Recover credentials',
        detail: 'Reset another account’s password or remove its second factor.',
        operator: true
      },
      {
        name: 'operator.ownership_transfer',
        label: 'Transfer ownership',
        detail: 'Hand the installation to another account.',
        operator: true
      }
    ]
  }
];

export const allCapabilities: Capability[] = capabilityGroups.flatMap(
  (group) => group.capabilities
);

export function capabilityLabel(name: string): string {
  return allCapabilities.find((capability) => capability.name === name)?.label || name;
}
