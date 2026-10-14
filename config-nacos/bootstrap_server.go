package nacos

// parseServersValue 解析结构化 server 列表。
//
// 说明：
// 1. 支持单个 map 与数组，便于 typed defaults 和 YAML/环境变量输入汇合；
// 2. 每个节点都在 bootstrap 阶段完成严格字段、类型和值域校验。
func parseServersValue(value any) ([]Server, error) {
	// 阶段一：统一输入容器形态，单 map 仍按一个节点处理。
	switch typed := normalizeValue(value).(type) {
	case map[string]any:
		server, err := parseServerMap(0, typed)
		if err != nil {
			return nil, err
		}
		return []Server{server}, nil
	case []map[string]any:
		// 阶段二：逐节点保留索引解析，确保错误能定位到声明位置。
		servers := make([]Server, 0, len(typed))
		for index, raw := range typed {
			server, err := parseServerMap(index, raw)
			if err != nil {
				return nil, err
			}
			servers = append(servers, server)
		}
		return servers, nil
	case []any:
		servers := make([]Server, 0, len(typed))
		for index, item := range typed {
			raw, ok := normalizeValue(item).(map[string]any)
			if !ok {
				return nil, errorForNacosInvalidBootstrap(messagef("invalid nacos server item server_index=%d type=%T reason=map_required", index, item))
			}
			server, err := parseServerMap(index, raw)
			if err != nil {
				return nil, err
			}
			servers = append(servers, server)
		}
		return servers, nil
	default:
		return nil, errorForNacosInvalidBootstrap(messagef("invalid nacos servers value type %T, expect map or array", typed))
	}
}

// parseServerMap 解析并校验一个结构化 Nacos 节点。
// 阶段角色：bootstrap 解码期间 fail-fast，并用 server_index 标识问题节点。
func parseServerMap(index int, raw map[string]any) (Server, error) {
	server := Server{}
	// 阶段一：严格识别节点字段并拒绝有损类型转换。
	for key, value := range raw {
		switch key {
		case "host":
			parsed, err := parseStringValue(value)
			if err != nil {
				return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server host server_index=%d", index), err)
			}
			server.Host = parsed
		case "port":
			parsed, err := parseUint64Value(value)
			if err != nil {
				return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server port server_index=%d", index), err)
			}
			server.Port = parsed
		case "scheme":
			parsed, err := parseStringValue(value)
			if err != nil {
				return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server scheme server_index=%d", index), err)
			}
			server.Scheme = parsed
		case "contextPath":
			parsed, err := parseStringValue(value)
			if err != nil {
				return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server contextPath server_index=%d", index), err)
			}
			server.ContextPath = parsed
		case "grpcPort":
			parsed, err := parseUint64Value(value)
			if err != nil {
				return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server grpcPort server_index=%d", index), err)
			}
			server.GrpcPort = parsed
		default:
			return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server field server_index=%d field=%s", index, key))
		}
	}
	// 阶段二：复用 SDK 节点校验，保证 ValidateBootstrap 与直接 NewClient 行为一致。
	if _, err := parseDeclaredServer(index, server); err != nil {
		return server, errorForNacosInvalidBootstrap(messagef("invalid nacos server declaration server_index=%d", index), err)
	}
	return server, nil
}

// encodeServers 把 typed 节点转换为 bootstrap map 列表。
// 阶段角色：Provider typed defaults 编码，保留显式字段并省略稳定默认值。
func encodeServers(servers []Server) []any {
	items := make([]any, 0, len(servers))
	// 节点零值表示稳定默认值，编码时省略以保留“未声明”语义。
	for _, server := range servers {
		item := map[string]any{"host": server.Host}
		if server.Port > 0 {
			item["port"] = server.Port
		}
		putIfNotEmpty(item, "scheme", server.Scheme)
		putIfNotEmpty(item, "contextPath", server.ContextPath)
		if server.GrpcPort > 0 {
			item["grpcPort"] = server.GrpcPort
		}
		items = append(items, item)
	}
	return items
}
