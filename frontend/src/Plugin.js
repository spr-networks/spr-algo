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
  ModalConfirm,
  Loading,
  Badge,
  BadgeText,
  Box,
  Button,
  ButtonText,
  CloseIcon,
  Icon,
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

const stateLabel = (s) =>
  ({
    running: 'Deploying…',
    success: 'Success',
    failed: 'Failed',
    interrupted: 'Interrupted',
    none: 'Never deployed'
  }[s] || s)

const Chip = ({ children, selected, onPress }) => (
  <Pressable onPress={onPress}>
    <Badge
      variant={selected ? 'solid' : 'outline'}
      action={selected ? 'success' : 'muted'}
      size="md"
    >
      <BadgeText>{children}</BadgeText>
    </Badge>
  </Pressable>
)

const LogTail = ({ text }) => {
  if (!text) {
    return (
      <Text size="xs" color="$muted500">
        No log output yet
      </Text>
    )
  }
  const lines = text.split('\n').slice(-200)
  return (
    <Box
      p="$3"
      borderRadius="$md"
      bg="$backgroundContentLight"
      sx={{ _dark: { bg: '$backgroundContentDark' } }}
      maxHeight={320}
      overflow="scroll"
    >
      {lines.map((line, i) => (
        <Text key={i} size="xs" style={{ fontFamily: 'monospace' }}>
          {line || ' '}
        </Text>
      ))}
    </Box>
  )
}

export default function Plugin() {
  const alert = useAlert()
  const [loading, setLoading] = useState(true)
  const [config, setConfig] = useState(null)

  // form state
  const [token, setToken] = useState('')
  const [region, setRegion] = useState('')
  const [serverName, setServerName] = useState('algo')
  const [users, setUsers] = useState([])
  const [newUser, setNewUser] = useState('')

  const [deploy, setDeploy] = useState(null)
  const [vpnConfigs, setVpnConfigs] = useState([])
  const [showDeploy, setShowDeploy] = useState(false)
  const prevState = useRef(null)

  const refreshConfig = useCallback(() => {
    return api
      .get(`${PLUGIN_BASE}/config`)
      .then((c) => {
        setConfig(c)
        setRegion(c.Region || '')
        setServerName(c.ServerName || 'algo')
        setUsers(c.Users || [])
      })
      .catch((err) => alert.error('Failed to load config', err))
  }, [])

  const refreshVpnConfigs = useCallback(() => {
    api
      .get(`${PLUGIN_BASE}/configs`)
      .then((list) => setVpnConfigs(list || []))
      .catch(() => {})
  }, [])

  const refreshDeploy = useCallback(() => {
    api
      .get(`${PLUGIN_BASE}/deploy/status`)
      .then((d) => {
        setDeploy(d)
        if (prevState.current === 'running' && d.State !== 'running') {
          refreshVpnConfigs()
        }
        prevState.current = d.State
      })
      .catch(() => {})
  }, [])

  useEffect(() => {
    Promise.all([refreshConfig(), refreshDeploy(), refreshVpnConfigs()]).finally(
      () => setLoading(false)
    )
    const t = setInterval(refreshDeploy, 3000)
    return () => clearInterval(t)
  }, [])

  const save = () => {
    const body = {
      Provider: 'digitalocean',
      DOToken: token, // empty = keep stored token
      Region: region,
      ServerName: serverName,
      Users: users
    }
    api
      .put(`${PLUGIN_BASE}/config`, body)
      .then((c) => {
        setConfig(c)
        setToken('')
        alert.success('Configuration saved')
      })
      .catch((err) => alert.error('Failed to save', err))
  }

  const addUser = () => {
    const name = newUser.trim()
    if (!name) return
    if (!/^[A-Za-z0-9_-]{1,32}$/.test(name)) {
      alert.error('User names: letters, digits, _ and - only (max 32)')
      return
    }
    if (users.includes(name)) {
      alert.error('Duplicate user')
      return
    }
    setUsers([...users, name])
    setNewUser('')
  }

  const startDeploy = () => {
    setShowDeploy(false)
    api
      .post(`${PLUGIN_BASE}/deploy`)
      .then(() => {
        alert.success('Deploy started')
        refreshDeploy()
      })
      .catch(async (err) => {
        let msg = 'Failed to start deploy'
        try {
          msg = await err.response.text()
        } catch (e) {}
        alert.error(msg)
      })
  }

  const triggerDownload = (href, filename) => {
    const a = document.createElement('a')
    a.href = href
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
  }

  const download = (cfg, png) => {
    const file = cfg.File + (png ? '.png' : '')
    api
      .get(`${PLUGIN_BASE}${file}`)
      .then((content) => {
        if (png) {
          // backend returns {PNGBase64} for QR images
          triggerDownload(
            `data:image/png;base64,${content.PNGBase64}`,
            `${cfg.User}.conf.png`
          )
        } else {
          const blob = new Blob([content], { type: 'text/plain' })
          triggerDownload(URL.createObjectURL(blob), `${cfg.User}.conf`)
        }
      })
      .catch((err) => alert.error('Download failed', err))
  }

  if (loading) {
    return (
      <Page>
        <Loading />
      </Page>
    )
  }

  const running = deploy?.State === 'running'
  const canDeploy =
    !running && (config?.DOTokenConfigured || token.length > 0) && region && users.length > 0

  return (
    <Page>
      <ListHeader
        title="Algo VPN"
        description="Deploy a personal WireGuard VPN server to DigitalOcean using Trail of Bits' Algo"
        mark="av"
        status={
          deploy?.State === 'success'
            ? 'Deployed'
            : running
            ? 'Deploying'
            : deploy?.State === 'failed'
            ? 'Failed'
            : 'Not deployed'
        }
        statusAction={
          deploy?.State === 'success'
            ? 'success'
            : running
            ? 'warning'
            : deploy?.State === 'failed'
            ? 'error'
            : 'muted'
        }
      >
        <Button size="sm" onPress={save}>
          <ButtonText>Save</ButtonText>
        </Button>
      </ListHeader>

      <Card>
        <SectionHeader
          title="Status"
          right={
            <StatusDot
              online={deploy?.State === 'success'}
              warn={running}
            />
          }
        />
        <HStack flexWrap="wrap" gap="$2">
          <StatTile label="Last deploy" value={stateLabel(deploy?.State || 'none')} />
          <StatTile
            label="Started"
            value={timeAgo(deploy?.StartedAt) || '—'}
          />
          <StatTile label="Users" value={String(users.length)} />
          <StatTile label="VPN profiles" value={String(vpnConfigs.length)} />
        </HStack>
      </Card>

      <Card>
        <SectionHeader title="Cloud Provider" />
        <VStack space="md">
          <KeyVal label="Provider" value="DigitalOcean" />
          <TextField
            label="API Token"
            value={token}
            onChangeText={setToken}
            placeholder={
              config?.DOTokenConfigured
                ? 'configured — enter a new token to replace'
                : 'dop_v1_…'
            }
            helper="Personal access token with read and write scopes. Stored on the router, never shown again."
            secureTextEntry
          />
          <TextField
            label="Server name"
            value={serverName}
            onChangeText={setServerName}
            placeholder="algo"
            helper="Name of the droplet Algo creates"
          />
          <VStack space="sm">
            <Text size="sm" bold>
              Region
            </Text>
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
          </VStack>
          <KeyVal label="VPN protocol" value="WireGuard (IPsec off in this version)" />
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="Users" count={users.length} />
        <VStack space="md">
          <Text size="sm" color="$muted500">
            One profile per device. Changing users requires a re-deploy to take
            effect on the server.
          </Text>
          <HStack flexWrap="wrap" gap="$2">
            {users.map((u) => (
              <Badge key={u} variant="outline" action="info" size="md">
                <BadgeText>{u}</BadgeText>
                <Pressable
                  ml="$1"
                  onPress={() => setUsers(users.filter((x) => x !== u))}
                >
                  <Icon as={CloseIcon} size="xs" />
                </Pressable>
              </Badge>
            ))}
            {users.length === 0 ? (
              <Text size="sm" color="$muted500">
                No users yet — add at least one
              </Text>
            ) : null}
          </HStack>
          <HStack space="sm" alignItems="flex-end">
            <Box flex={1}>
              <TextField
                label="Add user"
                value={newUser}
                onChangeText={setNewUser}
                placeholder="phone, laptop, …"
              />
            </Box>
            <Button size="sm" variant="outline" onPress={addUser}>
              <ButtonText>Add</ButtonText>
            </Button>
          </HStack>
        </VStack>
      </Card>

      <Card>
        <SectionHeader
          title="Deploy"
          right={
            <Button
              size="sm"
              action="primary"
              isDisabled={!canDeploy}
              onPress={() => setShowDeploy(true)}
            >
              <ButtonText>{running ? 'Deploying…' : 'Deploy'}</ButtonText>
            </Button>
          }
        />
        <VStack space="md">
          <Text size="sm" color="$muted500">
            Runs the Algo ansible playbook against DigitalOcean. Creates (or
            updates) the droplet, then generates one WireGuard profile per
            user. Takes about 5–10 minutes. Save the configuration first —
            deploys use the saved settings.
          </Text>
          {deploy && deploy.State !== 'none' ? (
            <VStack space="sm">
              <HStack space="md" flexWrap="wrap">
                <KeyVal label="State" value={stateLabel(deploy.State)} />
                <KeyVal label="Server" value={deploy.ServerName || '—'} />
                <KeyVal label="Region" value={deploy.Region || '—'} />
                {deploy.State === 'failed' ? (
                  <KeyVal label="Exit code" value={String(deploy.ExitCode)} />
                ) : null}
              </HStack>
              <LogTail text={deploy.LogTail} />
            </VStack>
          ) : null}
        </VStack>
      </Card>

      <Card>
        <SectionHeader title="WireGuard Profiles" count={vpnConfigs.length} />
        {vpnConfigs.length === 0 ? (
          <Text size="sm" color="$muted500">
            No profiles yet. They appear here after a successful deploy.
          </Text>
        ) : (
          <VStack space="sm">
            {vpnConfigs.map((cfg) => (
              <HStack
                key={cfg.Server + cfg.User}
                justifyContent="space-between"
                alignItems="center"
                flexWrap="wrap"
                gap="$2"
              >
                <VStack>
                  <Text size="sm" bold>
                    {cfg.User}
                  </Text>
                  <Text size="xs" color="$muted500">
                    server {cfg.Server}
                  </Text>
                </VStack>
                <HStack space="sm">
                  <Button size="xs" variant="outline" onPress={() => download(cfg, false)}>
                    <ButtonText>.conf</ButtonText>
                  </Button>
                  {cfg.HasQR ? (
                    <Button size="xs" variant="outline" onPress={() => download(cfg, true)}>
                      <ButtonText>QR .png</ButtonText>
                    </Button>
                  ) : null}
                </HStack>
              </HStack>
            ))}
          </VStack>
        )}
      </Card>

      <ModalConfirm
        isOpen={showDeploy}
        onClose={() => setShowDeploy(false)}
        onConfirm={startDeploy}
        title="Deploy Algo VPN server?"
        message={`This will create or update droplet "${serverName}" in ${region || '?'} on your DigitalOcean account (billed by DigitalOcean) and deploy WireGuard for ${users.length} user(s).`}
        confirmText="Deploy"
      />
    </Page>
  )
}
