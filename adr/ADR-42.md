# (Partitioned) Consumer Groups for JetStream

|Metadata| Value               |
|--------|---------------------|
|Date    | 2024-01-31          |
|Author  | @jnmoyne            |
|Status  | `Proposed`          |
|Tags    | jetstream, client   |

| Revision | Date       | Author   | Info           |
 |----------|------------|----------|----------------|
| 1        | 2024-01-31 | @jnmoyne | Initial design |

## Context and Problem Statement

Currently distributed message consumption from a stream is possible through the use of durable consumers, however the messages are distributed 'on-demand' between the client application processes currently subscribing to the consumer. This means that if you want strict in-order consumption of the messages you must set number of "max pending acknowledgements" to exactly 1, which seriously limits throughput as only one message can be consumed at a time, regardless of the number of client applications subscribing from the consumer. Moreover, even with max acks pending is set to 1, the current consumers do not provide any kind of 'stickiness' of the distribution of the messages between the subscribing client applications, nor the ability to have only one of those subscribing applications being selected to be the exclusive recipient of the messages (at a time).

While this is fine for most message consumption use cases, there are some classes of use cases that require strictly ordered 'per key' message consumption which are currently forced to set max acks pending to 1 (and therefore the consumption of the messages can not be scaled horizontally). Furthermore, even when strictly ordered message delivery is not required, there are other classes of use cases where 'per key' stickiness of the delivery of messages can be very valuable and lead to performance improvements. E.g. if the consuming application can leverage local caching of expensive lookup data stickiness enables the best possible cache hit-rate.

## Context

Kafka's 'consumer groups' provide this exact feature through partitioning, and it is an oft-requested by the NATS community (and by many wanting to migrate from Kafka to NATS JetStream). The ability to have an 'exclusive subscriber' to a consumer has also been one of the most often requested features from the community.

## Design goals

The design aims for the following attributes
- The ability to distribute the message consumption of the messages in a stream, while providing ***strictly ordered 'per key' delivery*** of the messages, when the key used for the consistent hashing distribution of the messages can be any combination of the subject's tokens.
- Fault-tolerance through the deployment of 'standby' consuming client applications that automatically take over the consumption of messages even in the case of process or host failure.
- ***Administratively managed*** 'elasticity': the ability to easily scale up or down the consumption of the messages by increasing or decreasing the number of running consuming client applications.
- ***Always*** keep the strict order of delivery per subject even when scaling up or down.
- Minimization of the re-distribution of keys as the consumer group is scaled up or down.
- - Support for both automatic or administratively defined mapping of members to partitions.

## Design

A number of new features of nats-server 2.10 are now making consumer groups (and exclusive consumption from a JetStream consumer) possible. Namely: subject transformation in streams with the ability to insert a partition number in a subject token calculated from other subject tokens using a consistent hashing algorithm, multi-filter JS consumers, create (as opposed to create or update) consumer creation call, meta-data in consumer configuration.

## Terminology

A ***consumer group*** is a named new kind of consumer on a stream that distributes messages between ***members*** which are client named application instances wanting to consume (copies of) messages in a stream in a partitioned way.

### Exposed functionalities

The following functionalities are exposed to the application programmer

- Get a named partitioned consumer group's configuration.
- Create a named partitioned consumer group on a stream by providing a configuration.
- Delete a named partitioned consumer group on a stream.
- List the partitioned consumer groups defined on a stream.
- Add members to a named partitioned consumer group.
- Drop members from a partitioned consumer group.
- List the partitioned consumer group's members that are currently consuming from the partitioned consumer group.
- Consume (if or when selected) from the partitioned consumer group.
- - Set (or delete) a custom member-to-partitions mapping.

### Partitioned consumer group configuration

The design relies on using a KV bucket to store the configuration information for the consumer groups. The key is the combination of the stream name and the partitioned consumer group's name. You can create any number of named partitioned consumer groups on a stream, just like you can create any number of consumers on a stream.

The configuration consists of the following:
- The maximum number of members that this consumer group will distribute messages to.
- A subject filter, that must contain at least one `*` wildcard.
- A list of the subject filter partitioning wildcard indexes that will be used to make the 'key' used for the distribution of messages. For example if the subject filter is `"foo.*.*.>"` (two wildcard indexes: 1 and 2) the list would be `[1,2]` if the key is composed of both `*` wildcards, `[1]` if the key is composed of only the first `*` wildcard of the filter, or `[2]` if is it composed of only the second `*` wildcard.
- The current list of partitioned consumer group member names or the current list of consumer group members to partition mappings.

Note: once the partitioned consumer group has been created, the list of members or the list member mappings are the ***only*** part of its configuration that can be changed.

#### Partitioned consumer group config record format

```go
type ConsumerGroupConfig struct {
MaxMembers            uint            `json:"max_members"`
Filter                string          `json:"filter"`
PartitioningWildcards []int           `json:"partitioning-wildcards"`
MaxBufferedMsgs       int64           `json:"msg-buffer-size,omitempty"`
Members               []string        `json:"members,omitempty"`
MemberMappings        []MemberMapping `json:"member-mappings,omitempty"`
}
```

### Partitioned consumer group members (consuming client applications)

Client applications that want to consume from the partitioned consumer group typically would either create the consumer group themselves (or rely on it being created administratively ahead of time) just like they would use a regular durable consumer. Then would then signal their ability to consume messages by consuming from (it could also be described as 'joining') the partitioned consumer group with a specific member name. Starting to 'consume' doesn't mean that messages will actually start being delivered to the client application, as that is controlled by two factors: the member's name being listed in the list or members in the partitioned consumer group's config, and if more than one instance of the same member are joined to the consumer group at the time, being selected as the 'exclusive' client application instance receiving the messages for that member's subset of the subjects in the stream. The member may start or stop receiving messages at any moment.

When starting to consume, besides a `context.Context` the client application programmer can provide a "Max acks pending" value (which defaults to and will typically be `1` when strictly ordered processing is required), a callback to process the message (same kind you would use for subscribing to a regular consumer), as well as optional callbacks to get notified when that particular instance of the member gets activated or de-activated as the membership of the group changes.

### Implementation design

People too often will associate elasticity of a partitioning system with having to change the number of partitions as the membership in the group changes over time, and re-partitioning is always going to be a difficult and expensive thing to do, and therefore not something you want to do all the time. In practice however there is way around that while not providing 'infinite elasticity' is still actually quite acceptable.

The trick is simply to see the number of partitions as the maximum number of members, and then you distribute those partitions as evenly as you can between the members, no need to try and be clever about it as the even distribution of the subjects is already done by the partitioning. Partitions in this case are cheap, they are literally nothing more than 'just another subject (filter)' so you don't have to try and be stingy with the number of them. Then you just need to have a coordinated way to move partitions between members, and you have elasticity.

These things are implemented on top of JetStream using new 2.10 features. The mapping of members to partitions is done by having each member create a named consumer named after the member name (with a relatively short idle timeout), and expressing the partitions that this particular member was assigned to as filter subjects in the consumer's configuration. The coordination trick is done by doing this on top of a working queue stream, which ensures that no two member consumers can have the same partition in their subject filters at the same time.

So when you create a consumer group, you actually create a new working queue stream, that sources from the initial stream and on which the members create consumers and inserts the partition number token in the subject at the same time. The obvious down-side is that this can lead to requiring the space to store the data twice in two streams, but the advantages are that guarantee that a working-queue stream will never allow two members to operate on the same partition at the same time, and it also gives you a convenient way to track the progress of the members in real-time (the size of that working-queue), which is something you could use to try to automate the scaling up or down, and finally using a working queue stream allows the creation/deletion of the consumers on that stream without having to worry about having to store and specify a starting sequence number in order to not re-consume messages.

The final part of the puzzle is the 'exclusive subscriber to a consumer' functionality: it's not just elasticity people want but also built-in fault-tolerance, so you need to be able to run more than one instance of each member name to protect from the member process/host/whatever failing causing messages to be unprocessed and accumulating in the consumer group until a new instance of that member name gets restarted. This synchronization is done relying on 'named ephemeral' consumers the new 2.10 Consumer Create (vs CreateOrUpdate) call with the addition of metadata in the consumer config: when an instance of member "m1" starts and wants to consume from the consumer group, it first generates a unique ID that it sets in the metadata of the consumer config and  uses `Consumer.Create()` to try to create a named (using the member's name) ephemeral 'synchronization consumer' on the consumer group's stream (the wq stream) with the subject filters for the partitions distributed to that member. If there's already another instance of member m1 that is running (and will have a different id), or if there's another member consumer on the stream that has overlapping filter subjects the Create call fails (because the metadata part of the consumer's config does not match), and the code just waits for a few seconds and re-tries. When that other instance of "m1" decides to stop consuming, it will delete that consumer, and another instance will be the first one to succeed in creating its version of that consumer. Because the consumer is created with an idle timeout of just a few seconds (and the currently active member instance constantly pulls on it), if the member instance crashes or gets disconnected then the server takes care of deleting the consumer.

#### Member consumer implementation detail
Using one single member consumer per member for both synchronization and getting messages does work well for failure scenarios where the member client application crashes, it breaks down if the client application gets suspended (same if isolated) because after resuming it will continue to pull from the shared synchronization consumer, not realizing that another member id now has it. To avoid this, two streams rather than one are used for each member, the 'synchronization' consumer and another instance-id specific 'data' consumer. They are configured the same, except the synchronization consumer has subject filters prepended by a token that is not a number (and therefore will not match any messages in the stream), while the data consumer uses subject filters that match the actual partitions. The implementation continuously pulls from both consumers (to keep them both non-idle) but no messages are received on the synchronization consumer and the stream messages are consumed from the data consumer.

The implementation when the instance is joining or not currently the selected instance checks every 5 seconds:
- Can I succeed in creating the consumers for the member
- And if they think they are the current selected member instance check that the consumers still exists, and that the instance id in the meta-data still matches. If not they should 'step-down' (drain their subscribers to both member consumers) and go back to assuming not being the currently elected instance.
- In order to implement a 'step-down' administrative command the current synchronization consumer simply gets deleted (note it could also be deleted by the server if the selected instance gets suspended or on the other side of a network partition), which causes the current selected instance to after a few seconds realize it's been deleted and drain it's current data consumer and assume not being the selected instance anymore, plus wait a second before going back to trying to create the synchronization consumer (to ensure the step-down causes another instance (if there's one at the time) to successfully create its version of the member consumers and become the new selected instance).

#### Details

The partition to member distribution is designed to minimize the number of partitions that get re-assigned from one member to another as the member list grows/shrinks: rather than being a simple modulo (which would suffice for distribution purposes) the algorithm is the following:

- De-duplicate the list of members in the consumer group's configuration
- Sort the list
- Cap the list to the max number of members
- (Unless a custom specific set of members to partition mappings is defined) Distribute automatically the partitions amongst the members in the list, assigning continuous blocks of partitions of size (number of partitions divided by the number of members in the list), and finally distribute the remainder (if the number of members in the list is not a multiple of the number of partitions) over the members using a modulo.

Automatic partition to member mapping implementation:

```go
func GetPartitionFilters(config ConsumerGroupConfig, memberName string) []string {
	members := deduplicateStringSlice(config.Members)
	slices.Sort(members)

	if uint(len(members)) > config.MaxMembers {
		members = members[:config.MaxMembers]
	}

	// Distribute the partitions amongst the members trying to minimize the number of partitions getting re-distributed
	// to another member as the number of members increases/decreases
	numMembers := uint(len(members))

	if numMembers > 0 {
		// rounded number of partitions per member
		var numPer = config.MaxMembers / numMembers
		var myFilters []string

		for i := uint(0); i < config.MaxMembers; i++ {
			var memberIndex = i / numPer
			if i < (numMembers * numPer) {
				if members[memberIndex%numMembers] == memberName {
					myFilters = append(myFilters, fmt.Sprintf("%d.>", i))
				}
			} else {
				// remainder if the number of partitions is not a multiple of the number of members
				if members[(i-(numMembers*numPer))%numMembers] == memberName {
					myFilters = append(myFilters, fmt.Sprintf("%d.>", i))
				}
			}
		}

		return myFilters
	}
	return []string{}
}
```

The partition number token is stripped from the subject before being passed down to the user's message consumption callback.

It is up to the client application consumption callback code to acknowledge the message. In order to achieve exactly once processing the client application callback code should follow this pattern:
1. Process the message begin a transaction on the destination(s) systems of record that get modified by the processing of the message, call prepare on all of them and if they all are prepared call `DoubleAck()` on the message, if that returns without an error then commit the transaction(s) but if the double ack does return an error (because for example the process was suspended or isolated for a period of time longer than what it takes for another member instance to be selected to consume the messages, in which case the newly active member instance will get all currently un-acknowledged messages from the consumer group's stream) then it should rollback the transaction(s) to avoid double-processing.

### Transitioning

Since when processing membership changes where partitions (and therefore subject filters) are moved from one consumer to another on a working queue stream it's impossible to synchronize all the currently active member instances to change their consumer configuration at exactly the same time, it's done in two steps. As soon as they get the config update, they first delete the consumers, and then wait a small amount of time before trying to create the new consumer again with the new filters. In order to avoid flapping of the active member instance on membership changes the currently active instance waits a fixed amount of time 500ms before trying to create the consumer while the others wait 500ms plus a random amount of time up to 100ms before trying.

## Administration

By simply using the consumer group config KV bucket, administrators can:
- Create consumer groups on streams -> creates a new config entry
- Delete consumer groups on streams -> deletes the config entry
- List consumer groups on a stream -> list the keys matching "<stream name>.*"
- Add/Remove members to a consumer group -> add/remove a member name to the `members` array in the config entry, or Create/Delete a set of custom member names to partition number mappings -> create/delete the `member-mappings` array in the config entry
- Request the current active member instance for a member to step-down and have another member instance currently joined (if there's one take over as the current active member instance)

You can also see the list of members that have currently active instances by looking at what consumer currently exist on the consumer group's stream, which is important because you therefore know when you are missing any and which ones are missing meaning that some of the partitions are not being consumed.

## Reference initial implementation
https://github.com/synadia-labs/partitioned-consumer-groups

## Consequences

Hopefully a lot of happy NATS users and lots of new NATS users moving off from other streaming systems that already have a partitioned consumer group functionality.