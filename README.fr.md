# MAX API

<div align="center">

![MAX API](./web/default/public/logo.png)

**AI Models and Agents governance, exploitation intelligente et collaboration ouverte pour l'ère des applications AGI**

**MAX API 2.0 : entrer dans l'ère de l'exploitation intelligente · évoluer d'une passerelle de modèles unifiée vers une infrastructure native de gouvernance et d'exploitation AGI**

[Consulter les notes de la dernière version stable](https://github.com/MAX-API-Next/MAX-API/releases/latest)

<p align="center">
  <a href="./README.zh_CN.md">简体中文</a> |
  <a href="./README.zh_TW.md">繁體中文</a> |
  <a href="./README.en.md">English</a> |
  <strong>Français</strong> |
  <a href="./README.ja.md">日本語</a>
</p>

<p align="center">
  <a href="https://raw.githubusercontent.com/MAX-API-Next/MAX-API/main/LICENSE">
    <img src="https://img.shields.io/github/license/MAX-API-Next/MAX-API?color=brightgreen" alt="licence">
  </a><!--
  --><a href="https://github.com/MAX-API-Next/MAX-API/releases/latest">
    <img src="https://img.shields.io/github/v/release/MAX-API-Next/MAX-API?color=brightgreen" alt="version">
  </a><!--
  --><a href="https://hub.docker.com/r/cscitechtop/max-api">
    <img src="https://img.shields.io/badge/docker-dockerHub-blue" alt="docker">
  </a><!--
  --><a href="https://goreportcard.com/report/github.com/MAX-API-Next/MAX-API">
    <img src="https://goreportcard.com/badge/github.com/MAX-API-Next/MAX-API" alt="GoReportCard">
  </a>
</p>

<p align="center">
  <a href="https://github.com/MAX-API-Next/MAX-API"><strong>⭐ Ajouter une Star</strong></a> •
  <a href="#-rejoindre-la-communauté-max-api-next"><strong>💬 Rejoindre la communauté</strong></a> •
  <a href="https://docs.max-api.ai"><strong>📚 Documentation</strong></a> •
  <a href="https://github.com/MAX-API-Next/MAX-API/releases"><strong>🚀 Versions</strong></a>
</p>

<p align="center">
  <a href="#-rejoindre-la-communauté-max-api-next">Communauté</a> •
  <a href="#-démarrage-rapide">Démarrage</a> •
  <a href="#-pourquoi-max-api">Pourquoi MAX API</a> •
  <a href="#-orientation-technique-pour-lagi">Orientation AGI</a> •
  <a href="#-capacités-actuelles">Capacités</a> •
  <a href="#-centre-dexploitation-intelligente">SmartOps</a> •
  <a href="#-guide-de-déploiement">Guide de déploiement</a>
</p>

</div>

---

MAX API se place entre les applications, les Agents et les services de modèles en amont. Il sert de passerelle de modèles unifiée, de plan de contrôle de gouvernance et de point d'entrée pour l'exploitation. La communauté MAX-API-Next développe le projet autour de l'ingénierie AGI concrète et accueille les développeurs, chercheurs, équipes d'ingénierie en entreprise, passionnés de technologie dans les universités et contributeurs open source. Notre objectif est de construire les fondations dont les applications AGI ont réellement besoin : accès aux modèles, permissions, maîtrise des coûts, Evidence, exploitation intelligente et contrôles de sécurité.

Dans le domaine de l'AGI, nous voulons **livrer en continu des technologies avancées et vérifiables, transformer les problèmes réels de production en capacités d'ingénierie ouvertes et bâtir une communauté de recherche et de contribution.**

## 🌐 Rejoindre la communauté MAX-API-Next

MAX API est plus qu'un dépôt de code. C'est une collaboration ouverte de long terme autour des AI Models, Agents, AgentOps et de l'ingénierie AGI. Que vous exploitiez une passerelle de modèles, développiez des Agents, adaptiez de nouveaux modèles, travailliez sur l'évaluation et la gouvernance, ou amélioriez la documentation et les traductions, vous êtes les bienvenus.

**Rejoignez la communauté pour suivre les versions, les évolutions de modèles et de protocoles, les pratiques de déploiement, les pistes de diagnostic et les possibilités de contribution.**

<p align="center">
  <strong>Groupe QQ : 950126533</strong> •
  <strong>Groupe WeChat : recherchez MAX-API</strong>
</p>

| Point d'entrée | Utilité |
|---|---|
| [MAX-API-Next sur GitHub](https://github.com/MAX-API-Next) | Suivre les projets communautaires, les orientations techniques et la collaboration ouverte |
| [Issues MAX API](https://github.com/MAX-API-Next/MAX-API/issues) | Signaler un problème reproductible, proposer une amélioration ou partager un changement de compatibilité |
| [Versions MAX API](https://github.com/MAX-API-Next/MAX-API/releases) | Suivre les mises à jour des versions stables |
| [Ask DeepWiki](https://deepwiki.com/MAX-API-Next/MAX-API) | Rechercher et comprendre rapidement le code |
| Coopération technique et écosystème | Contacter `maxapi@max-api.ai` |

### Profils de contributeurs recherchés

- **Contributeurs modèles et protocoles** : adaptation de nouveaux modèles et fournisseurs, Reasoning, appels d'outils et protocoles de tâches multimodales.
- **Développeurs d'Agents et d'applications** : pratiques d'intégration et de gouvernance pour Dify, RAGFlow, Codex, MCP, workflows et Agents de recherche.
- **Ingénieurs fiabilité et sécurité** : tests multi-bases, reprise des tâches asynchrones, sécurité des règlements, cohérence du cache et frontières de permissions.
- **Contributeurs documentation et communauté** : guides de déploiement, FAQ, exemples, traductions, expériences reproductibles et accueil des nouveaux contributeurs.
- **Chercheurs et développeurs d'évaluations** : Evidence, Evaluators, Detectors, Runbooks et architectures d'autonomie contrôlée.

<details>
<summary><strong>Voir les axes de collaboration, les principes et les façons de participer</strong></summary>

| Axe | Contributions possibles |
|---|---|
| Écosystème modèles et fournisseurs | Adaptateurs modèles/protocoles, tests de compatibilité, changements de listes et de dépréciation, modèles de canaux |
| AgentOps et gouvernance des applications AGI | Exemples d'intégration Agent, frontières de jetons et de permissions, gouvernance des coûts, pratiques d'exploitation |
| SmartOps et Evidence | Incidents anonymisés, définitions de métriques, règles d'alerte, qualité des données, méthodes de diagnostic et d'évaluation |
| Fiabilité, facturation et sécurité | Règlement idempotent, reprise asynchrone, régressions multi-bases, cohérence du cache, vérification des opérations à risque |
| Documentation, tutoriels et localisation | Déploiement, architecture, FAQ, cas d'usage, traductions et guides de contribution |

Fournissez si possible une reproduction minimale, la version, l'environnement, des journaux anonymisés ou des preuves de test. Les changements de protocole, de base de données ou de contrat utilisateur doivent décrire la compatibilité, la migration et le rollback. Ne soumettez pas de véritables clés API, données clients, enregistrements bruts de paiement, journaux privés ou données non autorisées. Les contributions touchant aux fonds, à la sécurité, aux secrets, aux données de production ou à l'exécution automatisée exigent des tests plus stricts, une revue indépendante et un responsable clairement identifié.

Vous pouvez commencer par :

1. Signaler un problème reproductible, une différence de protocole ou un changement de compatibilité d'un modèle.
2. Ajouter à une issue un test en échec, un cas multi-base, une régression frontend ou une Evidence anonymisée.
3. Améliorer la configuration des modèles/canaux, le déploiement, les FAQ, l'architecture ou les traductions.
4. Partager des pratiques anonymisées d'intégration Agent, de gouvernance des coûts, de SmartOps ou de déploiement privé.
5. Proposer un Evaluator, Detector, Runbook ou une architecture d'autonomie contrôlée avec des frontières de capacité et de risque explicites.

</details>

Documentation associée : [MAX-API-Docs](https://github.com/MAX-API-Next/MAX-API-Docs) (guides de déploiement, de configuration et d'utilisation).

Les fournisseurs de modèles, projets Agent/workflow, communautés open source, chercheurs et équipes d'ingénierie sont invités à collaborer. Les partenariats formels, annonces conjointes ou usages du nom d'une institution nécessitent l'autorisation explicite des deux parties.

> [!IMPORTANT]
> Lors de la fourniture au public de services d'IA générative, les opérateurs sont responsables des autorisations amont, déclarations ou licences, de la sécurité des contenus, de la vérification d'identité, de la conservation des journaux, de la fiscalité, des paiements, des conditions utilisateur et de toute obligation applicable dans leur juridiction. L'audit des journaux et la conservation de contenu ne doivent être activés qu'avec une base légale, une information claire, une isolation des permissions et des mesures adaptées de sécurité des données.

<img width="1902" height="1031" alt="Console d'administration MAX API" src="https://github.com/user-attachments/assets/fa481602-1e75-4326-9275-3c8271d01f5b" />

## 🧠 Orientation technique pour l'AGI

Les applications AGI ne dépendront pas éternellement d'un seul modèle, d'un seul protocole ou de requêtes ponctuelles. Elles nécessitent raisonnement multi-modèles, appels d'outils, tâches multimodales, exécutions longues, contraintes de coûts, Evidence de production et gestion récupérable des échecs. MAX API part de ces problèmes d'ingénierie vérifiables pour construire une fondation d'AI Models and Agents governance.

| Orientation technique | Fondations actuelles | Valeur pour les applications AGI |
|---|---|---|
| Accès multi-modèles et multi-protocoles | OpenAI Compatible, Responses, Claude Messages, Gemini, Realtime, protocoles multimodaux et tâches asynchrones | Offre aux applications et Agents un point d'entrée relativement stable malgré l'évolution de l'écosystème |
| Compatibilité du raisonnement et du contexte d'outils | Reasoning Effort, définitions d'outils, Tool Calls, association des réponses d'outils et conversion de contexte multi-tour | Réduit la perte d'informations de raisonnement et de sémantique des outils lors d'un changement de fournisseur |
| Plan de contrôle de gouvernance | Utilisateurs, jetons, périmètres de modèles, groupes, routage, limites, quotas, prix et permissions administrateur | Crée des frontières distinctes d'identité, d'accès et de budget pour chaque Agent, environnement et tâche |
| Facturation récupérable et cycle de vie des tâches | Préfacturation, règlement final, enregistrements idempotents, remboursement des échecs, polling asynchrone et états de rapprochement manuel | Évite doublons de débit, remboursements erronés et résultats intraçables pendant les longues tâches, retries et fenêtres d'échec |
| Evidence et exploitation intelligente | Journaux, erreurs, retries, buckets de performance, alertes actives, performance canal/modèle et preuves de règlement | Fonde diagnostic, évaluation et futures recommandations d'Agents sur des faits traçables plutôt que sur les seuls prompts |
| Sécurité et gouvernance organisationnelle | Passkeys, 2FA, revérification par périmètre, révocation de session, audit et limitation des opérations sensibles | Maintient une responsabilité explicite autour des configurations, secrets et opérations à risque |
| Autonomie contrôlée et évolution d'ingénierie | **Vision long terme** : Policy, Budget, Approval, Shadow, Canary, Rollback et Coding Workspaces isolés | Exige que l'automatisation soit évaluée, contrainte et auditée avant toute action de production à faible risque |

### Principes techniques

- **Evidence before Action** : établir des faits vérifiables avant tout diagnostic, recommandation ou action automatisée.
- **Governance before Autonomy** : identité, permissions, budgets, approbation, audit et rollback doivent précéder l'autonomie.
- **One Billing Truth** : facturation, quota et règlement de production conservent une source de vérité unique ; Agents et plugins ne créent pas une seconde comptabilité.
- **Compatibility by Design** : continuer à prendre en charge SQLite, MySQL, PostgreSQL, plusieurs protocoles fournisseurs et des contrats applicatifs portables.
- **Open Collaboration, Safe Boundaries** : ouvrir les adaptateurs, tests, documents, évaluations et conceptions de gouvernance, tout en maintenant les opérations de production, financières, de secrets et de publication sous approbation explicite.

### Points forts techniques de MAX API 2.0

- **Evidence-driven SmartOps** : regroupe alertes de ressources, performance canal/modèle, états de qualité des données et preuves de règlement dans un même point d'entrée, tout en conservant la frontière entre revue humaine et état financier réel.
- **Évolution continue pour les protocoles de modèles avancés** : améliore Reasoning, clés de cache, paramètres de pénalité, définitions d'outils et Tool Context multi-tour pour Responses, Claude Messages, Gemini et Ollama.
- **Sémantique récupérable de facturation et de tâches asynchrones** : règlement idempotent, effets persistants, identifiants de tâche et états pending/manual explicites empêchent les retries de créer doublons, remboursements incorrects ou pertes de tâche.
- **Revérification par périmètre pour les opérations à risque** : Passkeys, 2FA, Telegram, API Tokens et révocation de session utilisent une scope-bound step-up verification ; `session_generation` invalide rapidement les anciennes sessions.
- **Compatibilité multi-base et validation continue** : les chemins de données essentiels prennent en charge SQLite, MySQL et PostgreSQL ; tests Go, tests frontend Bun, vérification TypeScript, règle du wrapper JSON et synchronisation du miroir de tests constituent les barrières de publication.

## 💡 Pourquoi MAX API

| Dimension | Connexion directe à plusieurs fournisseurs | Avec MAX API |
|---|---|---|
| Intégration applicative | Maintenir des SDK, protocoles, authentifications et formats d'erreur distincts | Utiliser un point d'entrée applicatif relativement stable |
| Changement de modèle | Modifier le code, les secrets et la configuration de déploiement | Ajuster les canaux, mappings, groupes et règles de routage |
| Disponibilité | Chaque application gère ses retries et pannes amont | Centraliser poids, priorités, retries et bascules |
| Permissions et secrets | Secrets dispersés dans les applications et variables d'environnement | Gérer jetons, périmètres de modèles, quotas et expirations au même endroit |
| Comptabilité des coûts | Factures fournisseurs dispersées et difficiles à attribuer | Suivre l'usage par utilisateur, jeton, modèle, canal et groupe |
| Diagnostic | Journaux fragmentés entre fournisseurs | Observer requêtes, erreurs, retries et latence au niveau de la passerelle |

En une phrase : **les fournisseurs livrent les modèles, les frameworks Agent orchestrent l'application et MAX API unifie l'accès tout en faisant respecter les frontières de gouvernance.**

## 🚀 Démarrage rapide

L'expérience locale utilise SQLite par défaut et ne nécessite que Docker :

```bash
MAX_API_IMAGE=cscitechtop/max-api:latest@sha256:006d5d86887a261baab4d71ec3797d429e3771a4836e5899734aee0e7f66f2ab

docker pull "$MAX_API_IMAGE"

docker run --name max-api -d --restart always -p 127.0.0.1:3000:3000 -e TZ=Asia/Shanghai -v ./data:/data "$MAX_API_IMAGE"
```

Ouvrez ensuite <http://localhost:3000>, puis :

1. Créez ou confirmez le compte administrateur.
2. Ajoutez un canal amont et une clé API que vous êtes légalement autorisé à utiliser.
3. Créez un jeton d'accès et pointez la Base URL de votre application vers MAX API.

> [!TIP]
> Utilisez une version stable confirmée en production. Sauvegardez la base et préparez un rollback avant toute mise à niveau.
>
> [!WARNING]
> SQLite convient à l'évaluation locale, au développement et aux tests de petite taille. En production, utilisez des versions de MySQL ou PostgreSQL toujours couvertes par le support de sécurité du fournisseur (MySQL 8.4 LTS et PostgreSQL 14+ recommandés), avec Redis, HTTPS, sauvegardes et procédures de reprise. Les minimums de compatibilité restent MySQL ≥ 5.7.8 et PostgreSQL ≥ 9.6, mais ces versions ne sont pas recommandées en production.

## ✨ Capacités actuelles

Les capacités suivantes sont disponibles dans le système actuel :

| Capacité | Usage principal |
|---|---|
| Point d'entrée modèles unifié | Connecter OpenAI Compatible, Responses, Claude Messages, Gemini, Realtime et les interfaces de tâches multimodales |
| Routage multi-fournisseurs | Gérer canaux, poids, priorités, groupes, mappings, retries et bascule entre fournisseurs |
| Identité et contrôle d'accès | Gérer utilisateurs, jetons, périmètres de modèles, groupes, quotas, expiration, limitations et permissions administrateur |
| Coûts et facturation | Multiplicateurs, prix fixes, facturation par expression, rate-cards de tâches asynchrones, préfacturation, règlement et remboursement d'échec |
| Journaux et audit | Consulter usage, erreurs, retries et opérations d'administration par utilisateur, jeton, modèle, canal, groupe et nœud |
| Centre d'exploitation intelligente | Examiner alertes actives, performance des canaux et modèles, informations système et Evidence de rapprochement des règlements |
| Déploiement privé | SQLite, MySQL, PostgreSQL, Redis, plusieurs nœuds et base de journaux séparée |
| Extensibilité amont | Adaptateurs de protocole, overrides de chemin, paramètre/Header, découverte de modèles et mappings d'état de tâche |

### Cas d'usage

- **Passerelle interne pour une équipe ou organisation** : gérer utilisateurs, jetons, modèles, fournisseurs, permissions et coûts au même endroit.
- **Fondation d'exécution pour applications IA et Agents** : contrôle d'accès aux modèles, attribution des coûts et diagnostic pour applications, Agents et workflows.
- **Résilience et migration multi-fournisseurs** : réduire la dépendance à un seul amont grâce aux mappings, routage pondéré, retries et bascule progressive.
- **Gouvernance des tâches multimodales** : gérer images, audio, vidéo, embeddings, reranking et conversation temps réel.
- **Exploitation privée et conforme** : conserver la maîtrise des secrets, données, journaux, audits, prix et environnements de déploiement.

## 🩺 Centre d'exploitation intelligente

**Le Centre d'exploitation intelligente est une évolution majeure de MAX API 2.0 et une étape clé entre la passerelle de modèles unifiée et une infrastructure native de gouvernance et d'exploitation AGI.**

Il regroupe l'observation de production, les alertes de ressources, la performance des modèles et canaux, les informations système et le rapprochement des règlements de facturation dans une entrée administrateur unique. La capacité actuelle vise à voir les problèmes, préserver les preuves, notifier les administrateurs et permettre une revue contrôlée. Ce n'est pas un Agent autonome qui modifie automatiquement canaux, routage, soldes ou hôtes.

| Module | Contenu actuellement fourni |
|---|---|
| Alertes actives | Alertes dédupliquées lorsque CPU, mémoire ou disque du nœud courant reste au-dessus d'un seuil, avec notification de rétablissement ; réutilise la configuration Email, Webhook, Bark ou Gotify de l'administrateur |
| Performance des canaux | Requêtes et erreurs, quota consommé, taux de succès estimé, latence des journaux, retries, latence de probe et dernière observation ; le détail montre les performances modèles/groupes des dernières 24 heures |
| Performance des modèles | Nombre de canaux, requêtes et erreurs, quota consommé, taux de succès estimé, latence des journaux, débit et retries ; le détail fournit performances par groupe, tendances de latence et de disponibilité |
| Rapprochement des règlements | Affiche les règlements finaux positifs `pending` / `manual`, fonds non réglés, retries et preuves d'erreur ; les root administrators configurent la politique de blocage utilisateur par défaut et les administrateurs examinent atomiquement des lots par `id + revision` puis ferment les alertes |
| Informations système | Affiche les nœuds, instances actives, tâches système et informations associées ; ce module exige toujours le rôle super-administrateur |

La page des alertes actives lit l'état toutes les cinq secondes, mais ne déclenche ni nouvelle détection ni réparation. Les listes de canaux et modèles interrogent la dernière heure par défaut et acceptent une fenêtre personnalisée de `1–168` heures. Elles ne rescannent pas continuellement de grandes bases de journaux : la requête s'exécute uniquement avec « Appliquer les filtres » ou « Actualiser », et les détails sont chargés à la demande.

Le rapprochement des règlements sépare strictement l'état de reprise financière de l'état d'alerte opérationnelle. « Examiner et fermer » enregistre la revue et ferme l'alerte courante ; l'action ne marque pas le règlement comme `applied` et ne change ni le solde, ni l'écart appliqué, ni l'état de l'effet. La revue en lot est liée à la révision financière courante. Si un enregistrement change après actualisation, l'ancienne sélection devient invalide afin d'empêcher une action fondée sur une preuve obsolète.

> [!NOTE]
> Les vues de performance de production agrègent principalement les journaux Consume/Error et `perf_metrics`. Le taux de succès estimé n'est pas un taux complet de Relay Attempt ; débit et tendances sont des approximations par bucket. Lorsque les journaux sont désactivés, l'historique absent, la collecte arrêtée, la fenêtre vide ou la requête en échec, l'interface affiche l'état de qualité des données correspondant.
>
> Les alertes actives dépendent du suivi de performance et des seuils de ressources. Un seuil à `0` désactive l'alerte correspondante et deux échantillons valides consécutifs sont requis avant déclenchement. L'état et la file de notifications ne résident que dans la mémoire du processus courant : ils ne survivent pas au redémarrage et plusieurs nœuds ne les fusionnent pas en Incident multi-nœud. L'observation des canaux, modèles et du système reste en lecture seule. La revue de règlement ne met à jour que les métadonnées de revue et la politique de blocage utilisateur ; elle n'exécute aucun règlement financier. Le Centre d'exploitation intelligente ne teste, désactive, repondère, bascule ou répare jamais automatiquement.

Cette étape ferme d'abord la boucle « voir le problème, notifier l'administrateur, présenter les preuves, permettre une revue contrôlée », puis prépare une base pour Evidence unifiée, Agents de diagnostic, Evaluators et automatisation contrôlée.

## 🔌 Modèles, interfaces et extensibilité

> La disponibilité réelle dépend de vos autorisations amont, de la configuration des canaux, des mappings de modèles et du support fournisseur. MAX API gouverne ces capacités ; il ne fournit pas lui-même de service de modèles.

| Catégorie | Interface ou capacité |
|---|---|
| Interfaces générales | Chat Completions, Responses, Embeddings, Rerank, Images, Audio et Video |
| Protocoles natifs et temps réel | Claude Messages, Google Gemini, OpenAI Realtime et entrées associées |
| Raisonnement et appels d'outils | Reasoning Effort, outils fonctionnels, Tool Call IDs, noms d'outils et association multi-tour des réponses, avec conversion selon les capacités amont |
| Tâches asynchrones | Soumission, polling, mapping d'état, proxy de résultat et facturation paramétrée |
| Amonts personnalisés | Mappings de Base URL, chemin, paramètres, Header, champ d'état et champ de résultat |

La couverture inclut OpenAI, Claude, Gemini, Azure, AWS Bedrock, Vertex AI, Ollama et plusieurs plateformes de modèles chinoises. MAX API peut également gouverner Codex, Dify, RAGFlow et des services de tâches multimodales. Le périmètre exact dépend de la version et du type de canal.

### Flux système et stack technique

![Architecture système MAX API](./docs/images/MAX-API架构图.png)

```text
Application / SDK / Agent
  → Interface unifiée et authentification
  → Permissions modèles, limites, budgets et contrôles de sécurité
  → Sélection du canal, mapping et retry d'échec
  → Adaptation du protocole amont
  → Règlement récupérable, Evidence, journaux et audit
  → Centre d'exploitation intelligente et gouvernance administrateur
```

Le backend utilise Go, Gin et GORM. Le frontend utilise React 19, TypeScript, Base UI et Tailwind CSS. La couche de données prend en charge SQLite, MySQL et PostgreSQL, avec Redis et une base de journaux séparée en option. Les adaptateurs fournisseurs vivent dans une couche Relay/Channel dédiée, la facturation et le règlement restent dans une frontière de service unifiée, et SmartOps expose des observations en lecture seule avec des entrées de gouvernance limitées.

## 🛡️ Gouvernance et exploitation

Pour la production, configurez le système dans cet ordre :

1. Configurer la connexion, les limites de sécurité et la politique d'inscription.
2. Ajouter des canaux amont légalement autorisés et vérifier modèles, capacités et protocoles.
3. Configurer groupes, jetons, périmètres de modèles, quotas et prix par équipe, activité ou environnement.
4. Utiliser un jeton distinct par application, Agent ou environnement afin d'éviter le partage de secrets et l'ambiguïté d'attribution des coûts.
5. Configurer retries, journaux et alertes, puis observer le système avec les tableaux de bord et le Centre d'exploitation intelligente.

Pour la validation des capacités de canaux, la facturation par expression, les protocoles génériques de tâches, l'audit administrateur et les réglages de performance, consultez la [documentation](https://docs.max-api.ai).

## 🧭 Feuille de route

MAX API continuera de placer **AI Models and Agents governance** au cœur du projet. À partir de la passerelle unifiée, de l'exploitation intelligente et du règlement récupérable, il développera progressivement les capacités Evidence, évaluation, Policy et exécution contrôlée pour les applications AGI. L'objectif n'est pas de laisser un Agent sans frontières contrôler la production, mais de construire une boucle d'ingénierie vérifiable, approuvable, arrêtable et réversible.

| Étape | État | Priorité |
|---|---|---|
| Passerelle unifiée et exploitation intelligente | **Disponible** | Accès, authentification, routage, facturation, journaux, alertes de ressources, performance canal/modèle, informations système et rapprochement des règlements |
| Couche factuelle Evidence | **Développement proche** | Unifier requêtes de modèles, journaux système, métriques, Tasks, routage, Policies, règlements et événements d'audit derrière des interfaces Agent anonymisées, limitées et en lecture seule |
| Évaluations ouvertes et modèles de gouvernance | **Planifié** | Construire avec la communauté des tests de compatibilité, incidents anonymisés, jeux d'évaluation, Runbooks, Detectors et modèles de gouvernance métier |
| Exploitation autonome contrôlée | **Vision long terme** | Évaluer des actions automatisées à faible risque sous contraintes Policy, Budget, Approval, Shadow, Canary et Rollback |
| Évolution contrôlée des capacités | **Vision long terme** | Générer, tester et revoir des améliorations candidates dans des Coding Workspaces isolés sans modifier directement la production |
| Boucle d'ingénierie AGI | **Orientation long terme** | Relier Evidence, évaluation, politiques de gouvernance, approbation humaine et exécution réversible dans une boucle vérifiable |

MAX API n'est pas un modèle de fondation et ne prétend pas avoir déjà atteint l'AGI ni l'exploitation autonome. Les capacités de long terme ne seront validées progressivement qu'après mise en place des frontières d'Evidence, de permissions, de budget, d'approbation et de rollback.

## 🚢 Guide de déploiement

Les étapes suivantes prennent une version stable comme exemple. Figez le tag et le commit utilisés, puis préparez la sauvegarde, la vérification et le retour arrière avant toute mise à niveau.

Docker Compose est recommandé :

```bash
MAX_API_VERSION=v1.0.5
MAX_API_COMMIT=74a7ed3e4e2989c7e6f32b88a6b55e8446d2534e
git clone --branch "$MAX_API_VERSION" --depth 1 https://github.com/MAX-API-Next/MAX-API.git
cd MAX-API

actual_commit="$(git rev-parse HEAD)"
if [ "$actual_commit" != "$MAX_API_COMMIT" ]; then
  echo "Unexpected commit: $actual_commit" >&2
  exit 1
fi

# Modifier les mots de passe de base de données et Redis, puis configurer les secrets dans docker-compose.yml
docker compose up -d
```

### Vérifications de déploiement

| Composant | Recommandation |
|---|---|
| Base de données | Utiliser une version de MySQL ou PostgreSQL toujours couverte par le support de sécurité du fournisseur (MySQL 8.4 LTS et PostgreSQL 14+ recommandés), avec sauvegarde et reprise configurées |
| Cache | Le cache mémoire convient à un nœud ; Redis est recommandé pour plusieurs nœuds |
| Point d'entrée | Configurer un reverse proxy HTTPS, des limites de taille et une politique de réseau de confiance |
| Secrets | Définir explicitement un `SESSION_SECRET` aléatoire ; `CRYPTO_SECRET` est une surcharge facultative qui reprend `SESSION_SECRET` s'il est absent et doit être partagé entre Redis ou plusieurs nœuds lorsqu'il est défini |
| Nœuds | Donner à chaque nœud un `NODE_NAME` stable et unique |
| Journaux | Configurer `LOG_SQL_DSN`, nettoyage et rétention selon les exigences de conformité et d'exploitation |

Les déploiements multi-nœuds doivent partager `SESSION_SECRET`, tout en utilisant un `NODE_NAME` différent par nœud. `CRYPTO_SECRET` est une surcharge facultative qui reprend `SESSION_SECRET` s'il est absent ; s'il est défini explicitement, utilisez la même valeur sur chaque nœud. Utilisez `LOG_SQL_DSN` pour une base de journaux séparée et activez `ERROR_LOG_ENABLED` lorsque les statistiques de performance des erreurs sont nécessaires. Consultez la [documentation](https://docs.max-api.ai) pour toutes les variables d'environnement et les instructions de build depuis les sources.

## 🤝 Mentions légales et développements dérivés

Si vous créez ou distribuez une version dérivée, lisez intégralement [NOTICE](./NOTICE) et [LICENSE](./LICENSE), puis conservez les mentions légales, attributions, lien vers le projet d'origine et marquage des modifications qui y sont requis.

À ce stade, les projets dérivés qui respectent les mentions de MAX API et MAX-API-Next décrites ci-dessus bénéficient automatiquement et gratuitement d'une autorisation de développement dérivé accordée par le projet MAX API, sans demande ni approbation distincte. Cette politique ne constitue pas un engagement permanent et pourra être ajustée ultérieurement.

## 📜 Licence

Ce projet est distribué sous la [GNU Affero General Public License v3.0 (AGPLv3)](./LICENSE).

Si vous modifiez ce projet et le fournissez à des utilisateurs via un réseau, veuillez comprendre et respecter les obligations de mise à disposition du code source prévues par l'AGPLv3. Pour une coopération institutionnelle ou toute question de licence, contactez maxapi@max-api.ai.

---

<div align="center">

### 💖 Merci d'utiliser MAX API

Si ce projet vous aide, pensez à lui attribuer une ⭐ Star, suivre les Releases, signaler une Issue reproductible ou rejoindre la communauté MAX-API-Next.

**[Dépôt du projet](https://github.com/MAX-API-Next/MAX-API)** • **[Contribuer](https://github.com/MAX-API-Next/MAX-API/issues)** • **[Dernières versions](https://github.com/MAX-API-Next/MAX-API/releases)** • **[Communauté MAX-API-Next](https://github.com/MAX-API-Next)**

<sub>Built with ❤️ by MAX-API-Next</sub>

</div>
