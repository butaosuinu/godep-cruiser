module example.com/godep-cruiser-fixtures/third-party-in-core

go 1.25.0

require example.net/fixturedep v0.0.0

replace example.net/fixturedep => ./_deps/fixturedep
