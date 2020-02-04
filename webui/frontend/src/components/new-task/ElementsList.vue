<template>
  <v-card outlined>

    <v-card-title>
      {{ cardTitle }}
    </v-card-title>

    <v-card-subtitle v-if="cardSubtitle !== ''" align='left'>
      {{ cardSubtitle }}
    </v-card-subtitle>

    <v-card-text>

      <draggable v-model='userElementsComputed'>
        <div
          v-for="(userElement, index) in userElementsComputed"
          :key="userElement.ID + '_' + $uuid.v1()"
          class="my-2"
        >
          <v-card :elevation='4'>
            <v-card-title>
              {{ userElement.name }}
            </v-card-title>
            <v-card-subtitle>
              {{ userElement.description }}
            </v-card-subtitle>
            <v-card-text>
              <v-expansion-panels>
                <v-expansion-panel v-for="arg in userElement.args" :key="arg.ID">
                  <v-expansion-panel-header>
                    {{ arg.name }}
                  </v-expansion-panel-header>
                  <v-expansion-panel-content class="text--secondary text--darken-2">
                    {{ arg.description }}
                    <app-adaptative-arg
                      :content='arg.content'
                      :argType="arg.contentType"
                      @changed='arg.content = $event'
                    />
                  </v-expansion-panel-content>
                </v-expansion-panel>
              </v-expansion-panels>
            </v-card-text>
            <v-card-actions>
              <v-spacer/>
              <v-btn
                color='red lighten-1'
                @click="removeItem(index)"
                text
                icon
              >
                <v-icon>mdi-delete</v-icon>
              </v-btn>
            </v-card-actions>
          </v-card>
          <v-row v-if='userElement.order !== userElementsComputed.length - 1 && cardTitle === "Actions"' class="my-1" justify='center'>
            <v-icon color='blue darken-2' large>mdi-arrow-down-bold</v-icon>
          </v-row>
        </div>
      </draggable>

      <v-row justify='center'>
        <v-btn color='green' @click="openSelector" text icon>
          <v-icon>mdi-plus</v-icon>
        </v-btn>
      </v-row>
    </v-card-text>
  </v-card>
</template>

<script>
import draggable from 'vuedraggable'
import AdaptativeArgSelector from './AdaptativeArgSelector.vue'

export default {
  props: {
    cardTitle: {
      type: String,
      required: true
    },
    cardSubtitle: {
      type: String,
      required: false,
      default: ''
    },
    userElements: {
      type: Array,
      required: true
    }
  },
  data () {
    return {
    }
  },
  computed: {
    userElementsComputed: {
      get () {
        return this.userElements
      },
      set (newValue) {
        this.$emit('modified', newValue)
      }
    }
  },
  methods: {
    openSelector () {
      this.$emit('open-selector')
    },
    removeItem (elementIndex) {
      this.$emit('remove-item', elementIndex)
    },
    orderUpdateRequired () {
      this.$emit('order-modified')
    }
  },
  watch: {
    userElementsComputed: function (newValue) {
      this.orderUpdateRequired()
    }
  },
  components: {
    draggable,
    appAdaptativeArg: AdaptativeArgSelector
  }
}
</script>
