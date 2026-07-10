import React, { useCallback, useEffect, useRef, useState } from 'react'
import {
  api,
  useAlert,
  timeAgo,
  Page,
  ListHeader,
  Card,
  SectionHeader,
  StatTile,
  KeyVal,
  StatusDot,
  TextField,
  ModalForm,
  ModalConfirm,
  EmptyState,
  Loading,
  AlertCircleIcon,
  Badge,
  BadgeText,
  Box,
  Button,
  ButtonIcon,
  ButtonText,
  CheckIcon,
  CloseIcon,
  CopyIcon,
  GlobeIcon,
  Icon,
  Image,
  Input,
  InputField,
  HStack,
  Pressable,
  Text,
  VStack
} from '@spr-networks/plugin-ui'

const PLUGIN_BASE = `/plugins/${api.pluginURI() || 'spr-algo'}`

// Static DigitalOcean region list (kept in sync with DO's current slugs).
const DO_REGIONS = [
  { slug: 'nyc1', label: 'New York 1' },
  { slug: 'nyc3', label: 'New York 3' },
  { slug: 'tor1', label: 'Toronto 1' },
  { slug: 'sfo3', label: 'San Francisco 3' },
  { slug: 'atl1', label: 'Atlanta 1' },
  { slug: 'ams3', label: 'Amsterdam 3' },
  { slug: 'lon1', label: 'London 1' },
  { slug: 'fra1', label: 'Frankfurt 1' },
  { slug: 'blr1', label: 'Bangalore 1' },
  { slug: 'sgp1', label: 'Singapore 1' },
  { slug: 'syd1', label: 'Sydney 1' }
]

const reUser = /^[A-Za-z0-9_-]{1,32}$/
const reServerName = /^[A-Za-z0-9-]{1,32}$/

const STATE_WORD = {
  running: 'Deploying',
  success: 'Deployed',
  failed: 'Deploy failed',
  interrupted: 'Interrupted',
  none: 'Not deployed'
}

const PILL = {
  running: { label: 'Running', action: 'warning' },
  success: { label: 'Success', action: 'success' },
  failed: { label: 'Failed', action: 'error' },
  interrupted: { label: 'Interrupted', action: 'muted' }
}

// "6m 32s" between two RFC3339 stamps; end falls back to now (running jobs).
const fmtDuration = (startISO, endISO) => {
  const start = new Date(startISO || '').getTime()
  if (isNaN(start)) return null
  const end =
    endISO && !endISO.startsWith('0001') ? new Date(endISO).getTime() : Date.now()
  let s = Math.max(0, Math.floor((end - start) / 1000))
  const h = Math.floor(s / 3600)
  const m = Math.floor((s % 3600) / 60)
  s = s % 60
  if (h) return `${h}h ${m}m`
  if (m) return `${m}m ${s}s`
  return `${s}s`
}

const StatePill = ({ state }) => {
  const p = PILL[state]
  if (!p) return null
  return (
    <Badge action={p.action} variant="outline" borderRadius="$full" size="sm">
      <BadgeText>{p.label}</BadgeText>
    </Badge>
  )
}

const Chip = ({ children, selected, onPress }) => (
  <Pressable onPress={onPress}>
    <Badge
      variant={selected ? 'solid' : 'outline'}
      action={selected ? 'info' : 'muted'}
      size="md"
      borderRadius="$full"
    >
      <BadgeText>{children}</BadgeText>
    </Badge>
  </Pressable>
)

const CopyButton = ({ value, label = 'Value' }) => {
  const alert = useAlert()
  const copy = () => {
    navigator.clipboard
      .writeText(value)
      .then(() => alert.success(`${label} copied`))
      .catch(() => alert.error('Copy failed'))
  }
  return (
    <Button size="xs" variant="outline" action="secondary" onPress={copy}>
      <ButtonIcon as={CopyIcon} />
    </Button>
  )
}

// Collapsed-by-default terminal block for the ansible log tail.
const TerminalLog = ({ text }) => {
  const ref = useRef(null)
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight
  }, [text])
  const lines = (text || '').split('\n').slice(-200)
  return (
    <Box
      borderRadius="$lg"
      borderWidth={1}
      borderColor="$borderColorCardDark"
      bg="$backgroundContentDark"
      p="$3"
    >
      <div ref={ref} style={{ maxHeight: 300, overflowY: 'auto' }}>
        {text ? (
          lines.map((line, i) => (
            <Text
              key={i}
              size="xs"
              color="$textDark200"
              style={{ fontFamily: 'monospace', whiteSpace: 'pre-wrap' }}
            >
              {line || ' '}
            </Text>
          ))
        ) : (
          <Text size="xs" color="$muted400" style={{ fontFamily: 'monospace' }}>
            Waiting for log output…
          </Text>
        )}
      </div>
    </Box>
  )
}

// Numbered wizard step header with a done check.
const Step = ({ n, title, done, children }) => (
  <VStack space="sm">
    <HStack space="sm" alignItems="center">
      <Box
        w={26}
        h={26}
        borderRadius="$full"
        alignItems="center"
        justifyContent="center"
        bg={done ? '$primary600' : '$muted200'}
        sx={{ _dark: { bg: done ? '$primary500' : '$muted700' } }}
      >
        {done ? (
          <Icon as={CheckIcon} color="$white" size="2xs" />
        ) : (
          <Text size="xs" bold color="$muted600" sx={{ _dark: { color: '$muted300' } }}>
            {n}
          </Text>
        )}
      </Box>
      <Text size="sm" bold>
        {title}
      </Text>
    </HStack>
    <Box pl="$9">{children}</Box>
  </VStack>
)

// DigitalOcean token: "Configured ✓ / Replace" pattern, never echo the value.
const TokenEditor = ({ configured, token, setToken, replacing, setReplacing }) => {
  if (configured && !replacing) {
    return (
      <HStack space="sm" alignItems="center">
        <Badge action="success" variant="outline" borderRadius="$full" size="md">
          <BadgeText>Configured ✓</BadgeText>
        </Badge>
        <Button
          size="xs"
          variant="outline"
          action="secondary"
          onPress={() => setReplacing(true)}
        >
          <ButtonText>Replace token</ButtonText>
        </Button>
      </HStack>
    )
  }
  return (
    <VStack space="sm">
      <TextField
        label="DigitalOcean API token"
        value={token}
        onChangeText={setToken}
        placeholder="dop_v1_…"
        helper="Personal access token with read and write scopes — create one at cloud.digitalocean.com/settings/api/tokens. Stored on the router, never shown again."
        secureTextEntry
      />
      {configured ? (
        <Button
          size="xs"
          variant="outline"
          action="secondary"
          alignSelf="flex-start"
          onPress={() => {
            setToken('')
            setReplacing(false)
          }}
        >
          <ButtonText>Keep current token</ButtonText>
        </Button>
      ) : null}
    </VStack>
  )
}

const RegionPicker = ({ region, setRegion }) => (
  <HStack flexWrap="wrap" gap="$2">
    {DO_REGIONS.map((r) => (
      <Chip
        key={r.slug}
        selected={region === r.slug}
        onPress={() => setRegion(r.slug)}
      >
        {`${r.label} (${r.slug})`}
      </Chip>
    ))}
  </HStack>
)

// Add-on-enter chips editor for the user (device) list.
const UsersEditor = ({ users, setUsers }) => {
  const [draft, setDraft] = useState('')
  const [error, setError] = useState('')

  const add = () => {
    const name = draft.trim()
    if (!name) return
    if (!reUser.test(name)) {
      setError('Letters, digits, _ and - only (max 32)')
      return
    }
    if (users.includes(name)) {
      setError(`"${name}" is already in the list`)
      return
    }
    setUsers([...users, name])
    setDraft('')
    setError('')
  }

  return (
    <VStack space="sm">
      {users.length > 0 ? (
        <HStack flexWrap="wrap" gap="$2">
          {users.map((u) => (
            <Badge key={u} variant="outline" action="muted" size="md" borderRadius="$full">
              <BadgeText>{u}</BadgeText>
              <Pressable
                ml="$1.5"
                onPress={() => setUsers(users.filter((x) => x !== u))}
                accessibilityLabel={`Remove ${u}`}
              >
                <Icon as={CloseIcon} size="2xs" color="$muted500" />
              </Pressable>
            </Badge>
          ))}
        </HStack>
      ) : null}
      <HStack space="sm" alignItems="center">
        <Box flex={1}>
          <Input
            size="md"
            borderRadius="$xl"
            borderColor="$muted300"
            bg="$backgroundCardLight"
            sx={{ _dark: { bg: '$backgroundCardDark', borderColor: '$muted700' } }}
          >
            <InputField
              value={draft}
              onChangeText={(v) => {
                setDraft(v)
                if (error) setError('')
              }}
              onSubmitEditing={add}
              blurOnSubmit={false}
              placeholder="Add a user, e.g. alex-phone"
              autoCapitalize="none"
            />
          </Input>
        </Box>
        <Button size="sm" variant="outline" action="secondary" onPress={add}>
          <ButtonText>Add</ButtonText>
        </Button>
      </HStack>
      <Text size="xs" color={error ? '$error600' : '$muted500'}>
        {error ||
          'One profile per device. Press Enter to add. User changes apply on the next deploy.'}
      </Text>
    </VStack>
  )
}

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState(false)

  const [config, setConfig] = useState(null)
  const [status, setStatus] = useState(null)
  const [deploy, setDeploy] = useState(null)
  const [history, setHistory] = useState([])
  const [vpnConfigs, setVpnConfigs] = useState([])

  // config drafts
  const [token, setToken] = useState('')
  const [replacingToken, setReplacingToken] = useState(false)
  const [region, setRegion] = useState('')
  const [serverName, setServerName] = useState('algo')
  const [users, setUsers] = useState([])

  const [saving, setSaving] = useState(false)
  const [deployBusy, setDeployBusy] = useState(false)
  const [showDeployConfirm, setShowDeployConfirm] = useState(false)
  const [showLog, setShowLog] = useState(false)
  const [qr, setQr] = useState(null) // { user, server, uri }
  const [qrBusyFor, setQrBusyFor] = useState('')

  const prevState = useRef(null)
  const [, setTick] = useState(0)

  const refreshConfig = useCallback(() => {
    return api.get(`${PLUGIN_BASE}/config`).then((c) => {
      setConfig(c)
      setRegion(c.Region || '')
      setServerName(c.ServerName || 'algo')
      setUsers(c.Users || [])
    })
  }, [])

  const refreshStatus = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/status`)
      .then(setStatus)
      .catch(() => {})
  }, [])

  const refreshVpnConfigs = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/configs`)
      .then((list) => setVpnConfigs(list || []))
      .catch(() => {})
  }, [])

  const refreshHistory = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/deploys`)
      .then((list) => setHistory(list || []))
      .catch(() => {})
  }, [])

  const refreshDeploy = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/deploy/status`)
      .then((d) => {
        setDeploy(d)
        if (prevState.current === 'running' && d.State !== 'running') {
          refreshVpnConfigs()
          refreshHistory()
          if (d.State === 'success') alert.success('Deploy finished')
          else alert.error('Deploy did not finish', d.Error || d.State)
        }
        prevState.current = d.State
      })
      .catch(() => {})
  }, [])

  const loadAll = useCallback(() => {
    setLoading(true)
    setLoadError(false)
    return refreshConfig()
      .then(() =>
        Promise.all([
          refreshDeploy(),
          refreshVpnConfigs(),
          refreshHistory(),
          refreshStatus()
        ])
      )
      .catch(() => setLoadError(true))
      .finally(() => setLoading(false))
  }, [])

  useEffect(() => {
    loadAll()
    const t = setInterval(refreshDeploy, 3000)
    return () => clearInterval(t)
  }, [])

  const jobState = deploy?.State || 'none'
  const running = jobState === 'running'

  // 1s tick so elapsed time moves while a deploy runs
  useEffect(() => {
    if (!running) return
    const t = setInterval(() => setTick((x) => x + 1), 1000)
    return () => clearInterval(t)
  }, [running])

  const savedUsers = config?.Users || []
  const dirty =
    token !== '' ||
    region !== (config?.Region || '') ||
    serverName !== (config?.ServerName || 'algo') ||
    users.join('\n') !== savedUsers.join('\n')

  const serverNameError =
    serverName && !reServerName.test(serverName)
      ? 'Letters, digits and dashes only (max 32)'
      : ''

  const save = () => {
    setSaving(true)
    return api
      .put(`${PLUGIN_BASE}/config`, {
        Provider: 'digitalocean',
        DOToken: token, // empty = keep stored token
        Region: region,
        ServerName: serverName || 'algo',
        Users: users
      })
      .then((c) => {
        setConfig(c)
        setToken('')
        setReplacingToken(false)
        alert.success('Configuration saved')
        return c
      })
      .catch((err) => {
        alert.error('Failed to save', err)
        throw err
      })
      .finally(() => setSaving(false))
  }

  const startDeploy = () => {
    setDeployBusy(true)
    api
      .post(`${PLUGIN_BASE}/deploy`)
      .then(() => {
        alert.success('Deploy started')
        setShowLog(true)
        refreshDeploy()
        refreshHistory()
      })
      .catch(async (err) => {
        let msg = 'Failed to start deploy'
        try {
          msg = await err.response.text()
        } catch (e) {}
        alert.error(msg)
      })
      .finally(() => setDeployBusy(false))
  }

  // Wizard primary action: persist the drafts, then deploy.
  const saveAndDeploy = () => {
    save().then(() => startDeploy()).catch(() => {})
  }

  const triggerDownload = (href, filename) => {
    const a = document.createElement('a')
    a.href = href
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  const downloadConf = (cfg) => {
    api
      .get(`${PLUGIN_BASE}${cfg.File}`)
      .then((content) => {
        const blob = new Blob([content], { type: 'text/plain' })
        triggerDownload(URL.createObjectURL(blob), `${cfg.User}.conf`)
      })
      .catch((err) => alert.error('Download failed', err))
  }

  const openQR = (cfg) => {
    setQrBusyFor(cfg.Server + cfg.User)
    api
      .get(`${PLUGIN_BASE}${cfg.File}.png`)
      .then((content) =>
        setQr({
          user: cfg.User,
          server: cfg.Server,
          uri: `data:image/png;base64,${content.PNGBase64}`
        })
      )
      .catch((err) => alert.error('Failed to load QR code', err))
      .finally(() => setQrBusyFor(''))
  }

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  if (loadError) {
    return (
      <Page>
        <ListHeader
          title="Algo VPN"
          description="Deploy a personal WireGuard VPN server to DigitalOcean with Trail of Bits' Algo"
          mark="av"
        />
        <Card>
          <EmptyState
            icon={AlertCircleIcon}
            title="Can't reach the plugin backend"
            description="The spr-algo service isn't responding. It may still be starting — try again in a few seconds."
          >
            <Button size="sm" onPress={loadAll}>
              <ButtonText>Retry</ButtonText>
            </Button>
          </EmptyState>
        </Card>
      </Page>
    )
  }

  const everDeployed = jobState !== 'none' || history.length > 0
  const tokenReady = config?.DOTokenConfigured || token.length > 0
  const wizardReady = tokenReady && !!region && users.length > 0

  // saved config decides whether Deploy can run (deploys use saved settings)
  const savedReady =
    config?.DOTokenConfigured && !!config?.Region && savedUsers.length > 0
  const deployDisabledReason = running
    ? 'A deploy is already running.'
    : !config?.DOTokenConfigured
    ? 'Add your DigitalOcean API token in Configuration below.'
    : !config?.Region
    ? 'Pick a region in Configuration below.'
    : savedUsers.length === 0
    ? 'Add at least one user in Configuration below.'
    : ''

  const servers = [...new Set(vpnConfigs.map((c) => c.Server))]
  const byServer = {}
  vpnConfigs.forEach((c) => {
    byServer[c.Server] = byServer[c.Server] || []
    byServer[c.Server].push(c)
  })

  const headerStatus = STATE_WORD[jobState] || 'Not deployed'
  const headerAction =
    jobState === 'success'
      ? 'success'
      : running
      ? 'warning'
      : jobState === 'failed'
      ? 'error'
      : 'muted'

  // ---------- first run: guided setup ----------
  if (!everDeployed) {
    return (
      <Page>
        <ListHeader
          title="Algo VPN"
          description="Deploy a personal WireGuard VPN server to DigitalOcean with Trail of Bits' Algo"
          mark="av"
          status="Not set up"
          statusAction="muted"
        />
        <Card>
          <SectionHeader title="Set up your VPN server" />
          <VStack space="xl">
            <Text size="sm" color="$muted500">
              Three steps, then one deploy. The server runs in your own
              DigitalOcean account — SPR only orchestrates it.
            </Text>
            <Step n={1} title="Connect DigitalOcean" done={tokenReady}>
              <TokenEditor
                configured={config?.DOTokenConfigured}
                token={token}
                setToken={setToken}
                replacing={replacingToken}
                setReplacing={setReplacingToken}
              />
            </Step>
            <Step n={2} title="Pick a region" done={!!region}>
              <VStack space="sm">
                <RegionPicker region={region} setRegion={setRegion} />
                <Text size="xs" color="$muted500">
                  Closest region = lowest latency. The droplet is billed by
                  DigitalOcean.
                </Text>
              </VStack>
            </Step>
            <Step n={3} title="Add users" done={users.length > 0}>
              <UsersEditor users={users} setUsers={setUsers} />
            </Step>
            <VStack space="sm" pl="$9">
              <HStack space="sm" alignItems="center" flexWrap="wrap">
                <Button
                  size="sm"
                  action="primary"
                  isDisabled={!wizardReady || saving || deployBusy}
                  onPress={() => setShowDeployConfirm(true)}
                >
                  <ButtonText>
                    {saving || deployBusy ? 'Starting…' : 'Deploy VPN server'}
                  </ButtonText>
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  action="secondary"
                  isDisabled={!dirty || saving}
                  onPress={save}
                >
                  <ButtonText>{saving ? 'Saving…' : 'Save for later'}</ButtonText>
                </Button>
              </HStack>
              <Text size="xs" color="$muted500">
                {wizardReady
                  ? 'Runs Algo’s Ansible playbook against DigitalOcean. Takes about 5–10 minutes.'
                  : 'Complete the three steps above to deploy.'}
              </Text>
            </VStack>
          </VStack>
        </Card>
        <ModalConfirm
          isOpen={showDeployConfirm}
          onClose={() => setShowDeployConfirm(false)}
          onConfirm={saveAndDeploy}
          title="Deploy Algo VPN server?"
          message={`Creates droplet "${serverName || 'algo'}" in ${region} on your DigitalOcean account (billed by DigitalOcean) and generates WireGuard profiles for ${users.length} user${users.length === 1 ? '' : 's'}. Takes about 5–10 minutes.`}
          confirmText="Deploy"
        />
      </Page>
    )
  }

  // ---------- operational view ----------
  const lastJob = deploy && jobState !== 'none' ? deploy : null

  return (
    <Page>
      <ListHeader
        title="Algo VPN"
        description="Deploy a personal WireGuard VPN server to DigitalOcean with Trail of Bits' Algo"
        mark="av"
        status={headerStatus}
        statusAction={headerAction}
      />

      <Card>
        <SectionHeader
          title="Overview"
          right={<StatusDot online={jobState === 'success'} warn={running} />}
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile
            label="Server IP"
            value={
              servers.length === 0
                ? '—'
                : servers.length === 1
                ? servers[0]
                : `${servers.length} servers`
            }
            mono
          />
          <StatTile label="Region" value={config?.Region || '—'} mono />
          <StatTile label="Users" value={String(savedUsers.length)} />
          <StatTile label="Profiles" value={String(vpnConfigs.length)} />
          <StatTile label="Last deploy" value={timeAgo(deploy?.StartedAt) || '—'} />
          <StatTile
            label="Algo build"
            value={status?.AlgoCommit ? status.AlgoCommit.slice(0, 10) : '—'}
            mono
          />
        </HStack>
      </Card>

      <Card>
        <SectionHeader
          title="Deploy"
          right={
            <Button
              size="sm"
              action="primary"
              isDisabled={running || !savedReady || deployBusy}
              onPress={() => setShowDeployConfirm(true)}
            >
              <ButtonText>{running ? 'Deploying…' : 'Deploy'}</ButtonText>
            </Button>
          }
        />
        <VStack space="md">
          {lastJob ? (
            <HStack space="md" alignItems="center" flexWrap="wrap">
              <StatePill state={lastJob.State} />
              <Text size="sm" color="$muted500">
                {running
                  ? `Elapsed ${fmtDuration(lastJob.StartedAt) || '—'}`
                  : `${timeAgo(lastJob.StartedAt) || '—'} · took ${
                      fmtDuration(lastJob.StartedAt, lastJob.FinishedAt) || '—'
                    }`}
              </Text>
              <Text size="sm" color="$muted500" style={{ fontFamily: 'monospace' }}>
                {lastJob.ServerName} · {lastJob.Region}
              </Text>
              <Button
                size="xs"
                variant="outline"
                action="secondary"
                onPress={() => setShowLog(!showLog)}
              >
                <ButtonText>{showLog ? 'Hide log' : 'Show log'}</ButtonText>
              </Button>
            </HStack>
          ) : null}
          {deployDisabledReason ? (
            <Text size="xs" color="$muted500">
              {deployDisabledReason}
            </Text>
          ) : dirty ? (
            <Text size="xs" color="$muted500">
              You have unsaved configuration changes — deploys use the last
              saved settings.
            </Text>
          ) : (
            <Text size="xs" color="$muted500">
              Re-running a deploy updates the same droplet — use it after
              changing users. Takes about 5–10 minutes.
            </Text>
          )}
          {lastJob && lastJob.State === 'failed' ? (
            <KeyVal label="Exit code" value={String(lastJob.ExitCode)} mono />
          ) : null}
          {showLog && lastJob ? <TerminalLog text={lastJob.LogTail} /> : null}
        </VStack>
      </Card>

      {servers.length === 0 ? (
        <Card>
          <SectionHeader title="WireGuard profiles" count={0} />
          <EmptyState
            icon={GlobeIcon}
            title="No profiles yet"
            description="Each user gets a WireGuard profile after a successful deploy. Download the .conf or scan the QR code on the device."
          />
        </Card>
      ) : (
        servers.map((server) => (
          <Card key={server}>
            <SectionHeader
              title="WireGuard profiles"
              count={byServer[server].length}
              right={
                <HStack space="sm" alignItems="center">
                  <Text size="sm" color="$muted500" style={{ fontFamily: 'monospace' }}>
                    {server}
                  </Text>
                  <CopyButton value={server} label="Server IP" />
                </HStack>
              }
            />
            <VStack>
              {byServer[server].map((cfg, i) => (
                <HStack
                  key={cfg.User}
                  justifyContent="space-between"
                  alignItems="center"
                  flexWrap="wrap"
                  gap="$2"
                  py="$2.5"
                  borderTopWidth={i === 0 ? 0 : 1}
                  borderColor="$borderColorCardLight"
                  sx={{ _dark: { borderColor: '$borderColorCardDark' } }}
                >
                  <HStack space="sm" alignItems="center">
                    <Text size="sm" bold>
                      {cfg.User}
                    </Text>
                    <Text size="xs" color="$muted500">
                      WireGuard profile
                    </Text>
                  </HStack>
                  <HStack space="sm">
                    <Button
                      size="xs"
                      variant="outline"
                      action="secondary"
                      onPress={() => downloadConf(cfg)}
                    >
                      <ButtonText>Download .conf</ButtonText>
                    </Button>
                    {cfg.HasQR ? (
                      <Button
                        size="xs"
                        variant="outline"
                        action="secondary"
                        isDisabled={qrBusyFor === cfg.Server + cfg.User}
                        onPress={() => openQR(cfg)}
                      >
                        <ButtonText>Show QR</ButtonText>
                      </Button>
                    ) : null}
                  </HStack>
                </HStack>
              ))}
            </VStack>
          </Card>
        ))
      )}

      <Card>
        <SectionHeader
          title="Configuration"
          right={
            <Button
              size="sm"
              variant="outline"
              action="secondary"
              isDisabled={!dirty || saving || !!serverNameError}
              onPress={save}
            >
              <ButtonText>{saving ? 'Saving…' : 'Save'}</ButtonText>
            </Button>
          }
        />
        <VStack space="lg">
          <VStack space="sm">
            <Text size="sm" bold>
              DigitalOcean
            </Text>
            <TokenEditor
              configured={config?.DOTokenConfigured}
              token={token}
              setToken={setToken}
              replacing={replacingToken}
              setReplacing={setReplacingToken}
            />
          </VStack>
          <TextField
            label="Server name"
            value={serverName}
            onChangeText={setServerName}
            placeholder="algo"
            helper="Name of the droplet Algo creates or updates"
            error={serverNameError}
          />
          <VStack space="sm">
            <Text size="sm" bold>
              Region
            </Text>
            <RegionPicker region={region} setRegion={setRegion} />
          </VStack>
          <VStack space="sm">
            <Text size="sm" bold>
              Users
            </Text>
            <UsersEditor users={users} setUsers={setUsers} />
          </VStack>
          <KeyVal label="VPN protocol" value="WireGuard (IPsec off in this version)" />
        </VStack>
      </Card>

      {history.length > 0 ? (
        <Card>
          <SectionHeader title="Deploy history" count={history.length} />
          <VStack>
            {[...history]
              .reverse()
              .slice(0, 8)
              .map((job, i) => (
                <HStack
                  key={job.ID}
                  justifyContent="space-between"
                  alignItems="center"
                  flexWrap="wrap"
                  gap="$2"
                  py="$2"
                  borderTopWidth={i === 0 ? 0 : 1}
                  borderColor="$borderColorCardLight"
                  sx={{ _dark: { borderColor: '$borderColorCardDark' } }}
                >
                  <HStack space="md" alignItems="center" flexWrap="wrap">
                    <Text size="sm" minWidth={72}>
                      {timeAgo(job.StartedAt) || '—'}
                    </Text>
                    <Text size="sm" color="$muted500" style={{ fontFamily: 'monospace' }}>
                      {job.ServerName} · {job.Region}
                    </Text>
                    <Text size="xs" color="$muted500">
                      {job.State === 'running'
                        ? ''
                        : fmtDuration(job.StartedAt, job.FinishedAt) || ''}
                    </Text>
                  </HStack>
                  <StatePill state={job.State} />
                </HStack>
              ))}
            {history.length > 8 ? (
              <Text size="xs" color="$muted500" pt="$2">
                …and {history.length - 8} earlier
              </Text>
            ) : null}
          </VStack>
        </Card>
      ) : null}

      <ModalConfirm
        isOpen={showDeployConfirm}
        onClose={() => setShowDeployConfirm(false)}
        onConfirm={startDeploy}
        title="Deploy Algo VPN server?"
        message={`Creates or updates droplet "${config?.ServerName || 'algo'}" in ${config?.Region} on your DigitalOcean account (billed by DigitalOcean) and generates WireGuard profiles for ${savedUsers.length} user${savedUsers.length === 1 ? '' : 's'}. Takes about 5–10 minutes.`}
        confirmText="Deploy"
      />

      <ModalForm
        isOpen={!!qr}
        onClose={() => setQr(null)}
        title={qr ? `${qr.user} — scan with the WireGuard app` : ''}
      >
        {qr ? (
          <VStack space="md" alignItems="center" pb="$2">
            <Box bg="$white" p="$3" borderRadius="$lg">
              <Image
                source={{ uri: qr.uri }}
                alt={`WireGuard QR code for ${qr.user}`}
                w={240}
                h={240}
              />
            </Box>
            <Text size="xs" color="$muted500" textAlign="center">
              WireGuard app → Add tunnel → Scan from QR code. Anyone with this
              code can use the VPN as {qr.user}.
            </Text>
            <Button
              size="xs"
              variant="outline"
              action="secondary"
              onPress={() => triggerDownload(qr.uri, `${qr.user}.conf.png`)}
            >
              <ButtonText>Download PNG</ButtonText>
            </Button>
          </VStack>
        ) : null}
      </ModalForm>
    </Page>
  )
}
