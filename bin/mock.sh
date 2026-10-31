#!/bin/bash
# Generates the gomock mocks. Each Go module gets its own mocks/ directory: a
# module cannot import packages of another one, and the mocks are not
# versioned.
set -euo pipefail

cd "$(dirname "$0")/.."

go install go.uber.org/mock/mockgen@latest

rm -rf mocks agent/mocks

# Server module
mockgen -destination=mocks/common/clock/mock.go -package=mockClock github.com/alexandreh2ag/lacis/common/clock Clock
mockgen -destination=mocks/database/mock.go -package=mockDatabase github.com/alexandreh2ag/lacis/database Migrator
mockgen -destination=mocks/repository/mock.go -package=mockRepository github.com/alexandreh2ag/lacis/repository \
  NamespaceRepository,NetworkRepository,EnrollmentTokenRepository,AgentRepository,HostRepository,SnapshotRepository,UserRepository,RoleRepository,ProjectRepository,EnvironmentRepository,HostIPRepository,RuntimeRepository,ServiceRepository,EndpointRepository,WebserverRepository,AppRepository,AppDependencyRepository,InventoryRepository,LinkRepository,ChangeEventRepository,SessionRepository,TechIconRepository,Transactor

mockgen -destination=mocks/service/mock.go -package=mockService github.com/alexandreh2ag/lacis/service \
  UserService,EnrollmentTokenService,AgentService,SnapshotService,ImportService,NormalizeService,LinkService,GraphService,HistoryService,AuthService,NamespaceService,MetadataService,RoleService,TechIconService,TaskService,MaintenanceService
mockgen -destination=mocks/queue/mock.go -package=mockQueue github.com/alexandreh2ag/lacis/queue Queue,Admin
mockgen -destination=mocks/auth/openid/mock.go -package=mockOpenid github.com/alexandreh2ag/lacis/auth/openid Provider

# Agent module
(
  cd agent
  mockgen -destination=mocks/common/clock/mock.go -package=mockClock github.com/alexandreh2ag/lacis/common/clock Clock
  mockgen -destination=mocks/exec/mock.go -package=mockExec github.com/alexandreh2ag/lacis/agent/exec Executor
  mockgen -destination=mocks/client/mock.go -package=mockClient github.com/alexandreh2ag/lacis/agent/client Client
  mockgen -destination=mocks/kernel/mock.go -package=mockKernel github.com/alexandreh2ag/lacis/agent/kernel Kernel
)
