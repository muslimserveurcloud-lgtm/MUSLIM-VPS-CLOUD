# MUSLIM VPS CLOUD

Moteur d'orchestration VPS en Go : API REST, allocation de ressources, persistance JSON, moteur d'instances et pilote KVM/libvirt optionnel.

## Modes

- `MSC_HYPERVISOR=simulation` (défaut) : transitions logiques, aucun hyperviseur ni VM réelle.
- `MSC_HYPERVISOR=kvm` : appelle `qemu-img` et `virsh` sur un hôte Linux préparé avec KVM/libvirt.

**Important :** compiler le programme ou exécuter l'API ne crée pas un serveur physique. Le mode KVM exige un hôte Linux compatible, `/dev/kvm`, QEMU/libvirt, une image Linux cloud fiable, un réseau libvirt et des ressources physiques disponibles. Les builds GitHub Actions sont temporaires et ne sont pas un hébergement VPS permanent.

## Variables principales

- `PORT` (défaut `8080`)
- `MSC_CAPACITY_VCPU`, `MSC_CAPACITY_MEMORY_MB`, `MSC_CAPACITY_STORAGE_GB`
- `MSC_STATE_FILE` (défaut `data/allocations.json`)
- `MSC_ENGINE_STATE_FILE` (défaut `data/instances.json`)
- `MSC_HYPERVISOR=simulation|kvm`
- `MSC_KVM_STORAGE_DIR`, `MSC_KVM_BASE_IMAGE`
- `MSC_KVM_BASE_IMAGE_FORMAT=qcow2|raw`
- `MSC_KVM_URI`, `MSC_KVM_NETWORK`, `MSC_KVM_VIRSH_PATH`, `MSC_KVM_QEMU_IMG_PATH`

En KVM, définir les trois capacités selon les ressources réellement disponibles sur l'hôte.

## API

- `GET /health`
- `GET /v1/version`
- `GET /v1/hypervisor`
- `GET /v1/resources`
- `POST|GET /v1/instances`
- `GET /v1/instances/{id}`
- `POST /v1/instances/{id}/start|stop|restart`
- `DELETE /v1/instances/{id}` (instance arrêtée uniquement)

Création JSON :
`{"name":"demo","resources":{"vcpu":1,"memory_mb":1024,"storage_gb":10}}`

## Tests locaux ou CI

```sh
gofmt -w .
go vet ./...
go test ./...
go build ./cmd/server
```

## Limites explicites de cette version

Le pilote KVM crée un disque overlay et définit un domaine libvirt. Le provisionnement cloud-init, les clés SSH, les mots de passe, les snapshots, la console distante, l'attribution d'IP publique, le NAT/bridge, les quotas réels et la facturation ne sont pas encore implémentés. L'API ne doit pas être exposée publiquement sans authentification, TLS, validation des entrées et isolation réseau.
