### Template
[Release Template](https://github.com/opencloud-eu/opencloud/blob/master/.github/release_template.md)

### Prerequisites

* [ ] DEV/QA: Kickoff meeting [Kickoff meeting] (https://???)
* [ ] DEV/QA: Define client versions and provide list of breaking changes for desktop/mobile team
* [ ] DEV/QA: Check new strings and align with clients
* [ ] DEV/DOCS: Create list of pending docs tasks 
* [ ] DEV: Create branch `release-x.x.x-rc.x` -> CODEFREEZE
  * [ ] DEV: bump opencloud version in necessary files
  * [ ] DEV: `changelog/CHANGELOG.tmpl`
  * [ ] DEV: `pkg/version/version.go`
  * [ ] DEV: `sonar-project.properties` 
  * [ ] DEV: prepare changelog folder in `changelog/x.x.x_????_??_??`
* [ ] DEV: Check successful CI run on release branch
* [ ] DEV: Create signed tag `vx.y.z-rc.x`
* [ ] DEV: Check successful CI run on `vx.y.z-rc.x` tag / BLOCKING for all further activity
* [ ] DEV: Merge back release branch
* [ ] DEV: bump released deployments to `vx.y.z-rc.x`
* [ ] DEV: https://cloud.opencloud.eu/
  * [ ] DEV: needs snapshot and migration

### QA Phase

* [ ] QA: Confirmatory testing (if needed)
* [ ] QA: [Compatibility test](???)
* [ ] QA: [Performance test](https://github.com/opencloud-eu/cdperf/tree/main/packages/k6-tests/src)
* [ ] QA: Documentation test:
  * [ ] QA: Single binary - setup
  * [ ] QA: Docker - setup
  * [ ] QA: Docker-compose - setup
  * [ ] QA: helm/k8s - setup
* [ ] QA: e2e with different deployment:
  * [ ] QA: [wopi](???.works) 
  * [ ] QA: [traefik](???.works)
  * [ ] QA: [ldap](???.works)
* [ ] QA: e2e with different storage:
  * [ ] QA: local
  * [ ] QA: nfs
  * [ ] QA: s3
* [ ] QA: Different clients:
  * [ ] QA: desktop (define version) https://github.com/opencloud-eu/client/releases
    * [ ] QA: against mac - smoke test
    * [ ] QA: against windows - smoke test
    * [ ] QA: against linux (use auto tests)
  * [ ] QA: android (define version) https://github.com/opencloud-eu/android/releases
  * [ ] QA: ios (define version)
* [ ] QA: [Smoke test](???) on Web Office (Collabora, Onlyoffice, Microsoft office)
* [ ] QA: Smoke test Hello extension
* [ ] QA: [Smoke test](???) ldap
* [ ] QA: Collecting errors found

### After QA Phase

* [ ] Brief company-wide heads up via mail @tbsbdr
* [ ] Create list of changed ENV vars and send to release-coordination@opencloud.eu
  * [ ] Variable Name
  * [ ] Introduced in version
  * [ ] Default Value
  * [ ] Description
  * [ ] dependencies with user other components
* [ ] DEV: Create branch `release-x.x.x`
  * [ ] DEV: bump OpenCloud version in necessary files
  * [ ] DEV: `pkg/version/version.go`
  * [ ] DEV: `sonar-project.properties`
  * [ ] DEV: released deployment versions
  * [ ] DEV: prepare changelog folder in `changelog/x.x.x_???`
* [ ] Release Notes + Breaking Changes @tbsbdr
* [ ] Migration + Breaking Changes Admin Doc @???
* [ ] DEV: Create final signed tag
* [ ] DEV: Check successful CI run on `vx.y.z` tag / BLOCKING for all further activity
* [ ] Merge release notes 

### Post-release communication
* [ ] DEV: Create a `docs-stable-x.y` branch based on the docs folder in the OpenCloud repo @micbar 
* [ ] DEV/QA: Ping documentation in RC about the new release tag (for opencloud/helm chart version bump in docs)
* [ ] DEV/QA: Ping marketing to update all download links (download mirrors are updated at the full hour, wait with ping until download is actually available)
* [ ] DEV/QA: Ping @??? once the demo instances are running this release
* [ ] DEV: Merge back release branch
* [ ] DEV: Create stable-x.y branch in the OpenCloud repo from final tag
