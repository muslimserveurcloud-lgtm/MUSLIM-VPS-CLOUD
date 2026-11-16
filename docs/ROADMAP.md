# Roadmap technique

## Inclus dans cette archive
- API REST standard library
- allocation persistante de vCPU/RAM/stockage
- cycle de vie des instances
- mode simulation explicite
- interface de pilote hyperviseur
- pilote KVM/libvirt de première intégration
- tests unitaires et compilation multi-architecture dans GitHub Actions

## Prochaines étapes avant une bêta exploitable
1. Réconcilier l'état persistant avec `virsh domstate` au redémarrage du service.
2. Ajouter une vraie transaction de suppression et récupération après panne.
3. Ajouter cloud-init : hostname, utilisateur, clés SSH, réseau et premier démarrage.
4. Ajouter un réseau isolé, firewall, NAT/bridge et collecte d'adresse IP.
5. Ajouter authentification API, rôles admin/client, TLS, audit logs et rate limiting.
6. Ajouter quotas, facturation et limites par compte.
7. Ajouter snapshots, console sécurisée, métriques et sauvegardes.
8. Tester sur un hôte Linux x86_64 avec KVM/libvirt réel, puis valider ARM séparément.

## Critères de bêta
- aucun accès API non authentifié en production
- tests d'échec et de récupération
- tests réels KVM documentés
- provisioning SSH/cloud-init vérifié
- isolation réseau et stockage vérifiée
- sauvegarde/restauration validée
